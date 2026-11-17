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
as an `openapi/undecodable-source` error, and nothing compiles. Every input is read as a stream: the
source, an [overlay](#overlays) and a document a reference reads. Each takes the first of its
documents that holds content; the documents after it that hold content reach the IR in no form,
reported once as an `openapi/stream-documents-dropped` error in that input's own
`Document.Sources` entry at the first of them. The source still lowers, and a reference into an
external document's first document is read, but an overlay is refused, as one that does not parse
is ([`overlays`](#overlays)): nothing is applied and the source compiles to no Document. A
document holding nothing, a bare separator or an explicit null, is not counted. A read counts 1,024
documents past the one it takes and then reads one more, so a stream that continues past them is
reported, with the count as a floor, whether or not the documents it counted hold content.
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

A source, a document a reference reads or an [overlay](#overlays) that is one JSON text is read as
JSON, into the tree the YAML parser builds for the same text
([`json-as-yaml`](../parser-library.md#json-as-yaml)), because that parser refuses or misreads some
JSON that RFC 8259 allows. It refuses a character outside the Basic Multilingual Plane written as a
surrogate pair of `\u` escapes, a `\/` escape, a key whose `:` stands more than 1,024 characters
past the key's start (a key of more than 1,022 characters written `"key":`), a raw DEL, U+FFFE,
U+FFFF or a C1 control other than U+0085 in a string, and a tab before the root value or on a line
after the one it ends on, and it reads a raw U+0085 as a space. Each compiles as the same text
written with the characters themselves, at the positions the text writes: a line ends at LF, CR or
CR LF alone, as JSON tools and editors end one, so a raw U+0085, U+2028 or U+2029 in a string ends
none, where the parser ends a line at each, and a column counts characters. An escape that names
half of a surrogate pair, one not paired with the other half it needs, names no character: it
refuses the source as an `openapi/undecodable-source` error, the document a reference names as an
`openapi/unresolved-ref` error, and an overlay as an `openapi/invalid-overlay` error, each with a
message that names the escape as written and the line and column it begins at. Text that is not one
JSON text, YAML or JSON followed by anything else, is read as YAML, as before, its errors included.

Every YAML text is read as YAML 1.2, as YAML 1.2 asks of a processor whatever a `%YAML` directive
declares ([`yaml11-lexing`](../parser-library.md#yaml11-lexing)): a `\/` escape is a slash in a
double-quoted scalar and the two characters it writes anywhere else, as a pair of `\u` escapes
naming the two halves of a surrogate pair is the one character it names there, as JSON spells one,
and the twelve characters as written anywhere else, while a half alone is refused as in JSON text
above; a raw U+0085, U+2028 or U+2029 is a character and ends no line, and a `%YAML 1.2`
directive is read. A document declaring a later
minor is read as 1.2 with an `openapi/yaml-version-reading` warning at the directive, by position
alone, and one declaring another major is refused as the parser refuses it. A document declaring
1.1 draws the same warning at each plain scalar YAML 1.1's types read as another JSON value than
1.2's core schema, `0644`, `yes` or `1:30`, and at each scalar tagged `!!int` the two read as two
integers, `!!int 0644`, a scalar past 128 characters compared and worded by
kind alone and its text not quoted, and at each line-break character written raw in its text or in
a comment; an escaped `\N`, which YAML 1.1 reads alike, draws none, and a document declaring
nothing or 1.2 draws none. A UTF-16 or UTF-32 text opening with its byte order mark, JSON or YAML,
is read as the UTF-8 it encodes, so all three read alike. A plain scalar is resolved by YAML 1.2's core schema in
every document: `0644` is the integer 644, `0o17` is 15, and `0b11`, `0X1F`, `1_000` and `+0x1F`
are strings; its text is what the source wrote. An `0o` or `0x` integer past 64 bits is the
exception: the parser tags it a string, and the compiler reads it as one, which a bound such as
`maximum` refuses and a count such as `maxLength` keeps verbatim under `Unmodeled`. A mapping key
keeps the parser's tag, a date's
aside, which is a string as a value's is, and a `<<` key still merges. A core integer the parser
tags a float, `089` or one past 64 bits, keeps that tag, since the parser's model refuses to decode
an `!!int` it cannot hold: its integrality is read from its text, by `value.NumericLiteral` and the
rule table alike, and no reader may take it from the tag.

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

- Cases: `yaml-timestamp-scalars`, `numeric-precision`
- Codes: `openapi/undecodable-source`, `openapi/stream-documents-dropped`, `openapi/tagged-mapping`,
  `openapi/budget-exceeded`, `openapi/unresolved-ref`, `openapi/invalid-overlay`,
  `openapi/yaml-version-reading`
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
  `load.TestExternal_DocumentPreparesTheFile`,
  `doctree.TestReadJSON_BuildsTheTreeYAMLBuilds`, `doctree.TestReadJSON_ReadsWhatYAMLRefusesOrMisreads`,
  `doctree.TestReadJSON_ReportsALoneSurrogateWhereItIsWritten`,
  `doctree.TestReadJSON_DeclinesWhatIsNotOneJSONText`,
  `doctree.TestReadJSON_ReadsNestingAtTheDecoderBound`, `doctree.TestReadJSON_StopsOnceTheContextIsDone`,
  `doctree.TestReadJSON_ReportsExactlyTheSurrogateEscapesAsHalves`,
  `doctree.TestReadJSON_ReadsAFaultAfterAnEscapeAsTheFaultAlone`,
  `doctree.TestReadJSON_KeepsTheDecodersReasonWhenItDeclines`,
  `doctree.TestRead_AJSONTextReadsAlikeInEveryEncoding`,
  `doctree.TestRead_ATailIsWhatTheStreamHoldsPastItsFirstDocument`,
  `doctree.TestRead_AnEscapedSlashReadsAsASlashOnlyWhereTheStyleSaysSo`,
  `doctree.TestRead_ALineBreakYAML11EndsALineAtIsACharacter`,
  `doctree.TestRead_ADirectiveIsTheOneGoverningTheDocumentTaken`,
  `doctree.TestRead_AYAML11DocumentRecordsWhatItReadsOtherwise`,
  `doctree.TestRead_AYAML11DocumentRecordsAnIntegerTaggedOutrightThatReadsApart`,
  `doctree.TestRead_ACoreScalarIsRetaggedAndNeverRewritten`,
  `e2e.TestCompile_AnOverlayStreamWithASecondDocumentIsRefused`,
  `e2e.TestCompile_AnExternalDocumentStreamIsReportedInItsOwnEntry`,
  `e2e.TestCompile_AStreamPastTheBoundIsAlwaysReported`,
  `e2e.TestCompile_AnEscapedSlashReadsAsASlash`, `e2e.TestCompile_ALineBreakInACommentEndsNoLine`,
  `e2e.TestCompile_ALaterYAMLMinorIsReadAsYAML12WithAWarning`,
  `e2e.TestCompile_ADocumentDeclaringYAML11IsWarnedAtWhatItReadsOtherwise`,
  `e2e.TestCompile_ABaseTenIntegerIsReadInBaseTen`,
  `e2e.TestCompile_NoScalarTextOrKeyIsRewrittenByTheCoreSchema`,
  `load.TestDecode_ReadsAJSONSourceAsJSON`, `load.TestDecode_ReturnsALoneSurrogateAsItIs`,
  `load.TestDecode_AJSONTextBesideAStreamIsReadAsYAML`, `load.TestParseTree_ReadsJSONAsJSON`,
  `load.TestReadJSON_ADoneContextEndsTheReadWithoutAYAMLParse`,
  `load.TestReadJSON_AContextDoneByTheEndOfTheReadIsCtxsError`,
  `load.TestLoad_AJSONDocumentBehindARespelledURLIsReadAsJSON`,
  `detect.TestRecognize_AJSONSourceWritingASurrogatePairIsRecognized`,
  `detect.TestRecognize_ALoneSurrogateIsReportedWhereItIsWritten`,
  `e2e.TestCompile_AnEscapedJSONSourceCompilesAsItsLiteralTwin`,
  `e2e.TestCompile_AJSONSourceWithATabBeforeTheRootCompilesAsItsTwin`,
  `e2e.TestCompile_AJSONSourceWithAKeyPastTheYAMLLimitCompiles`,
  `e2e.TestCompile_AJSONExternalDocumentCompilesAsItsLiteralTwin`,
  `e2e.TestCompile_ALoneSurrogateIsRefusedWhereItIsWritten`,
  `e2e.TestCompile_APositionAfterAnEscapeCountsItAsWritten`,
  `e2e.TestCompile_ASurrogatePairOfEscapesInYAMLIsTheCharacterItNames`,
  `e2e.TestCompile_ALoneHalfOfASurrogatePairIsStillRefused`,
  `e2e.TestCompile_ASurrogatePairInAKeyAndBesideOtherRespellingsReadsEachAsItsOwn`,
  `doctree.TestRead_ASurrogatePairIsTheCharacterOnlyInADoubleQuotedScalar`,
  `doctree.TestRead_ASurrogatePairAfterAnEvenRunOfBackslashesIsNotOne`,
  `doctree.TestRead_AnUnpairedHalfIsRefusedAsItIsToday`, `doctree.TestRead_ASurrogatePairInAKeyIsTheCharacter`
- Status: partly built: both order oracles in `internal/harness` parse a source as YAML to permute
  it as YAML 1.1 lexes it, and decline one only the compiler's reader reads as not YAML: a JSON
  text writing a surrogate pair, and a YAML text writing an escaped slash, a pair of surrogate
  escapes or a `%YAML 1.2` directive (#1768). A plain `0o` or `0x` integer past 64 bits reads as
  the string the parser tags it, which a bound such as `maximum` refuses and a count such as
  `maxLength` keeps verbatim (#1975). A text writing every one of the 64 characters from
  U+E000 that the reader stands in for what it reads as YAML 1.2, and a UTF-16 or UTF-32 text
  encoding no text, are read as the parser reads them.

### `source-budgets`

`Options.Limits` bounds how large a source the compiler lowers, and how much its lowering copies,
each budget taking its default when unset and none when negative. A source past `MaxSourceBytes` is
refused before it is parsed, as one past the budget rather than one of a size: `MaxSourceBytes` is
the compiler's `SourceLimit`, so a caller may hand detection only its first bytes, one past the
budget. A document holding more than `MaxSourceNodes` nodes is refused as it is read: a JSON text
at the first node past the budget, which bounds what reading it costs, a YAML text once it is
parsed; the source, the overlay and each document a reference reads alike, so an overlay that would
shrink a source past the budget does not lift it. What an overlay adds is bound by `overlay.Apply`,
which checks each action against the same budget before it builds anything, so the tree it leaves
is within the budget too. A
source whose aliases add more than `MaxAliasSurplus` nodes to it is refused before its model is
built. Each is an `openapi/budget-exceeded` error, compiling to no
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
its aliases expanded, each counted as the keys and indexes it holds are written, a `/` or `~` in a
key once where the pointer's escape spells two bytes, total past a fixed multiple of the bytes it
writes, with a floor for a small
document, as an `openapi/pointer-amplification` error at the document, since the compile spells a
pointer for each node it reads and the IR's IDs embed them. An overlay is held to the same refusals
before it is applied, and so is a document an external reference names, which fails the reference
instead; each is read no further than the byte budget. The member budget of an enum is
[`enum-const`](#enum-const)'s.

`MaxReferenceExpansion` bounds what the lowering copies through `$ref`s, the product of references
and size the budgets above leave free. A declaration a `$ref` names, a schema aside, lowers again at
each `$ref` naming it, so each such `$ref` is charged what the declarations its chain names weigh:
the one it ends at, and each component alias it passes through, whose fields beside its own `$ref`
lower at every use too ([`reference-object-siblings`](#reference-object-siblings),
[`path-item-ref-fields`](#path-item-ref-fields)), and the use itself. An alias, and the mapping
that writes the use's `$ref`, each weigh one node for the mapping and what it writes beside its
`$ref`, weighed as a declaration is: the `$ref` is read, not copied, so a bare use and each bare
alias of its chain weigh one node, and a `$ref` merged in through `<<` is weighed as written. A
declaration weighs a node for each YAML node, a
YAML alias as a copy of what it names, and a byte for each byte of scalar text, but a schema it
holds weighs one node whatever the schema holds: it lowers once, to the type every copy refers to. A
schema is weighed whole beneath a merge key, which the grammar reads as raw YAML, beneath an
Encoding Object that another nests, which a copy keeps verbatim
([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)), and wherever the
grammar cannot place the `$ref`'s position or reads no object a `$ref` names there. Every `$ref` is
charged, the first to a declaration and one nested in another's among them, since each lowers a
copy, a link's among them, since each response naming one lowers its own. A schema's `$ref` names
one registry entry ([`schema-registry`](#schema-registry)), so it is not charged. A path item's
operations copy what the item declares itself. Its fixed methods bound those copies, and OpenAPI
3.2's `additionalOperations` does not, so each operation that field declares is charged too, the
first among them, which charges an item the same in any order of its entries: an inline parameter of
the item that it takes, weighed in the document writing it as a `$ref`'s declaration is, the entries
it keeps of the item, a byte for each byte of their keywords and raw values and one between, the
item's summary and description its `PathDocs` holds ([`path-item-docs`](#path-item-docs)), a byte
for each byte of their keywords and text and one between, and the IDs of the item's servers where it
names none of its own ([`path-item-servers`](#path-item-servers)). Past the budget no `$ref` lowers,
nor such an operation, and the compile is refused as one `openapi/budget-exceeded` error at the
source and no position in it, compiling to no Document, beside what the load phase found and nothing
the lowering did: which copy crossed the budget, and so what lowered before it, follows the walk's
order. With no Document to list the documents `$ref`s read, what the load phase found in one of them
is left out too.

- Codes: `openapi/budget-exceeded`, `openapi/source-too-deep`, `openapi/source-too-large`,
  `openapi/alias-amplification`, `openapi/pointer-amplification`

The scalar text a document's aliases add to it, its text with every alias substituted less the text
it writes, is held to the byte budget: a document whose aliases add past it is refused,
`openapi/budget-exceeded` at the document, `YAML aliases add <n> bytes of scalar text to the
document, past the <budget>-byte budget`, the source, an overlay document, the tree an overlay
patches and each document a reference reads alike. The node budgets weigh no text, so a few
aliases of a long scalar could otherwise expand into gigabytes within every one. A document with
no alias adds nothing, however its escapes decode.

What a `$ref` reaches outside the source's own reading is checked once for each kind it is read as,
whichever reference reaches it first: an object inside a container already checked as that kind is
not read again, and an extension's content, which no container reads as an object, is. So the
work is at most the kinds times the nodes the node budget admits, and no budget of its own is
charged. The keys of each other document are checked once, over each outermost object reached in
it.

A source past the pointer bound, as written or as an overlay patches it, is weighed again before it
is refused, by what a compile names one by one: a value the grammar keeps raw, an example, a
default, a `const`, an enum member, an Example Object's value, an `x-` key or a field its object
leaves out, adds its root alone, and each key a mapping in it writes more than once, where the
check places a finding ([`validation`](#validation)), though not an Encoding Object another nests:
the lowering keeps it verbatim ([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)), but the
binder reads it node by node for the references written in it. A schema's `$vocabulary` and
`dependentRequired`, which no model holds and the check judges entry by entry, are named node by
node, as its `required` is. The total judges no entry, so it names those the check accepts, which
a compile spells no pointer for, beside those it places a finding at: a document past the bound
that holds most of its nodes in such entries is refused though a compile of it is cheap. A
mapping's members are read as the binder reads
them, each a `<<` merge key brings in named at the mapping, and a key written twice at the copy the
binder reads, a mapping's own at its last and a merged source's at its first
([`validation`](#validation)), so a schema merged into a schema's position is named node by
node, and one under a copy the binder passes over is not. A raw value a `$ref`, a `$dynamicRef`, or
a value of a discriminator's `mapping` or its `defaultMapping` lands in, its fragment read as the
binder reads one, percent escapes decoded, is named node by node as the kind that reference
expects, once for each position it lands at that the grammar does not read as that kind, since the
binder walks each from where it lands and an anchor can mount one value at many. A discriminator's
value is read by its text whatever its tag, as the binder reads one, and one spelling the key of a
schema component names that component, which is read where it is declared. A key of a Security
Requirement Object that names no security scheme component is such a reference to a Security
Scheme Object, the key its URI, in a document of a minor that reads one so, from 3.2
([`security-requirements`](#security-requirements)). It is refused only when that total too is past
the bound, or when it cannot be taken: a
reference leaves
the document, names nothing its pointer finds, passes a reference object that writes a `$ref`, where
the binder reads on in the object it names and not beside the `$ref` ([`references`](#references)),
a path item's own fields not told apart, or is read under an `$id`, which moves where it
lands; a fragment is no JSON pointer; a chain of `<<` merge keys runs past the depth the binder
expands; the document may name a dialect, by `jsonSchemaDialect` or a
`$schema` key anywhere, whose keywords hold schemas the minor's grammar keeps raw (draft 7's
`definitions`, `dependencies`, an `items` list and `additionalItems`); a mapping writes a key that
is no scalar, or a `<<` that names no mapping, where the check of the document's keys spells a
pointer for every node to place its findings; or the weighing runs past the node bound. An overlay
document and a document a reference reads keep the first total.

That total is a second reading of what a compile walks, and two generated tests hold it to the
walks: `bind.TestNamedPointerBytes_TotalsWhatATablesWalksSpell` counts the pointer of every frame
the binder's walks push, and `e2e.TestCompile_SpellsNoPointerTheNamedTotalLeavesOut` what a whole
compile allocates more as a key above a value grows, so a walk spelling a pointer at each node of
a value counted by its root fails one of them. Each holds the documents its grammar writes and no
others: neither reads a document naming a dialect or another document, nor one whose total is not
taken (`bind.TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed`), which the refusals
above leave to the first total. The first counts frames, so a pointer a walk spells without pushing
one is not counted: the binder spells one for a requirement key that is a URI, for a tuple's entry
and for a schema's `discriminator`, each at a frame it pushes or one token below a schema's. The
second holds each minor and counts what is allocated, not what is held at once.
- Tests: `openapi.TestCompile_SourceByteBudgetBindsOnlyPastTheLimit`,
  `openapi.TestCompiler_SourceLimitReadsTheCallersOptions`,
  `openapi.TestDetect_DeclinesASourceHoldingMoreNodesThanTheCallersBudget`,
  `e2e.TestCompile_ASourceIsHeldToTheNodeBudgetAsItIsRead`,
  `e2e.TestCompile_AnOverlayDoesNotLiftTheNodeBudgetOfTheSource`,
  `e2e.TestCompile_AnOverlayIsHeldToTheNodeBudgetAsItIsRead`,
  `e2e.TestCompile_AnExternalDocumentIsHeldToTheNodeBudgetAsItIsRead`,
  `doctree.TestRead_AJSONTextPastTheNodeBudgetStopsWhereItCrosses`,
  `doctree.TestRead_AYAMLTextPastTheNodeBudgetIsCountedWhole`,
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
  `openapi.TestCompile_AnAliasIsChargedBesideItsEnd`, `service.TestExpansion_ChargesEveryCopy`,
  `service.TestExpansion_AHopWeighsWhatItWritesBesideItsRef`,
  `service.TestDeclared_ChargesEachAliasWhatItWritesBesideItsRef`, `e2e.TestCompile_ALongRefChainCompiles`,
  `e2e.TestCompile_AChainFarPastTheExpansionBudgetIsRefused`,
  `service.TestWeightOf_CostsTheBudgetNotTheDeclaration`,
  `service.TestWeightOf_CountsASchemaAsOneNode`,
  `service.TestDeclared_ChargesEachDeclarationOfAChain`,
  `service.TestDeclared_ResolvesNoReferencePastTheExpansionBudget`,
  `openapi.TestCompile_APathItemsAdditionalOperationsAreChargedWhatTheyCopy`,
  `openapi.TestCompile_APathItemsFixedMethodsAreNotCharged`,
  `openapi.TestCompile_AnItemsAdditionalOperationsAreChargedInAnyOrder`,
  `openapi.TestCompile_AnItemsParameterIsWeighedInItsOwnDocument`,
  `operation.TestLowerService_NoAdditionalOperationLowersPastTheBudget`,
  `e2e.TestCompile_ScalarTextAliasesAddIsHeldToTheByteBudget`,
  `e2e.TestCompile_ScalarTextAliasesAddIsHeldToTheBudgetAtItsEdge`,
  `e2e.TestCompile_AnOverlayDocumentIsHeldToTheAliasTextBudget`,
  `e2e.TestCompile_AnExternalDocumentIsHeldToTheAliasTextBudget`,
  `e2e.TestCompile_AnObjectReachedInsideAContainerAndAsTheContainerIsCheckedOnce`,
  `load.TestResolve_AReachedObjectIsChargedToNoBudget`, `oas.TestCheck_ANodeSeenIsSkippedWhenItIsReadAgain`,
  `oas.TestCheck_SkipLeavesUnreadAMappingItAnswersTrueFor`,
  `e2e.TestCompile_ARawValueNestingLongKeysIsNotRefusedAsPointerAmplification`,
  `e2e.TestCompile_ARawValueAReferenceLandsInIsNamedNodeByNode`,
  `e2e.TestCompile_APropertyNamedExampleHoldsASchemaAndIsNamedNodeByNode`,
  `bind.TestNamedPointerBytes_TheTotalDoesNotDependOnWhichMountIsWrittenFirst`,
  `bind.TestNamedPointerBytes_AFieldNoModelHoldsIsNamedNodeByNode`,
  `bind.TestNamedPointerBytes_AMemberAMergeKeyBringsInIsNamedAtTheMapping`,
  `bind.TestNamedPointerBytes_AReferenceIntoARawValueNamesItNodeByNode`,
  `bind.TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed`,
  `bind.TestNamedPointerBytes_AFragmentLandsWhereItsDecodingNames`,
  `bind.TestNamedPointerBytes_ALandingNamedInPlaceIsCountedOnce`,
  `bind.TestNamedPointerBytes_ALandingIsNamedAtEachPositionItIsReachedAt`,
  `bind.TestNamedPointerBytes_AKeyWrittenTwiceIsNamedAtTheCopyAMergeReads`,
  `bind.TestNamedPointerBytes_ADynamicRefLandsAsARefDoes`,
  `bind.TestNamedPointerBytes_ADiscriminatorValueLandsAsTheBinderReadsIt`,
  `bind.TestNamedPointerBytes_ARequirementKeyLandsAsTheBinderReadsIt`,
  `bind.TestNamedPointerBytes_ALandingIsNamedAsTheKindItsReferenceExpects`,
  `bind.TestNamedPointerBytes_TotalsEachPositionATableReads`,
  `bind.TestNamedPointerBytes_TotalsWhatATablesWalksSpell`,
  `bind.TestNamedPointerBytes_AKeyARawValueWritesTwiceIsNamed`,
  `bind.TestNamedPointerBytes_AKeywordJudgedByEntryIsNamedNodeByNode`,
  `load.TestNamedWithin_ADocumentBreakingTheFormatRulesIsNotWithin`,
  `e2e.TestCompile_SpellsNoPointerTheNamedTotalLeavesOut`,
  `e2e.TestCompile_ARawValueARequirementKeyNamesIsNamedNodeByNode`,
  `e2e.TestCompile_ARawValueAReferenceLandsInIsWeighedHoweverItIsSpelled`

### `overlays`

An OpenAPI Overlay document handed in as `Options.Overlay` is applied to the source's parsed tree
before its model is built. Its bytes are the caller's, or, when it hands none, read in the compile
from the local file `Overlay.Path` names, through the caller's reader and unconfined, since the
caller named it and not the spec. One the reader cannot read is a Go error, as an unreadable spec
is, and so are an overlay naming no file and one naming an http(s) URL, since no setting takes a
compile onto the network. Once applied, the pre-parse refusals and the node budgets hold the patched
tree as they hold the source, and validation and every lowering read it. A refusal at a node the
overlay grafted names the overlay, and so does a refusal of the whole patched tree, such as its
pointers' bytes, since the source passed the same refusals alone; its message says it reads the tree
the overlay leaves, which the overlay's own refusal does not. An overlay that is one JSON text is
read as JSON, as a source is ([`yaml-decoding`](#yaml-decoding)), so the characters and escapes the
YAML parser refuses compile as the characters they name.

An overlay declaring Overlay 1.0, 1.1 or 1.2, at any patch, which the specification asks tooling
not to tell apart, is applied by that version's rules, 1.2 by 1.1's, which it restates. The overlay
document is read by the fields its version's objects define, each in the form the specification
gives it and read as the compiler reads a scalar, with `x-` fields taken as extensions: a field of
another form, a required one missing, and a field the version's object does not define, such as a
misspelled `update` or a `copy` at 1.0, are each an `openapi/invalid-overlay` error at the field,
and nothing is applied, since an overlay exists to change the output and one applying as nothing
would ship the SDK it was written to fix. So are any other version and an overlay that does not
parse, including one writing half of a surrogate pair, and at 1.2 an `extends` or `$self` holding a
fragment; one that is no URI reference is refused at any version. An Overlay 1.2 action writing
`$ref` is a reference to a reusable action of `components.actions`, named by a JSON pointer in the
document's own fragment, its key escaped as RFC 6901 escapes a token: it applies as the action the
reusable one's `fields` write, at the reference's own `target`, a `description` of either having no
effect, as an action's has none, and a reference naming no entry, or writing `update`, `remove` or `copy`, is refused, as
are `fields` writing a `target`. A reusable action nothing references has no effect. The overlay's
`$self` is recorded as its `SelfURI` in `Document.Sources`; nothing the compiler applies reads it,
and `extends` is checked as a URI reference and compared with nothing.

Every `target` and `copy` is read as an RFC 9535 JSONPath query by the compiler's own evaluator, over
the JSON value the source stands for: a YAML alias reads as the value it names, a member a `<<`
merge key brings in as a member of the mapping merging it, and a scalar as the compiler reads one,
so a plain date is the string it spells, `0x10` the number 16, and two objects are equal whatever
the order of their members. A selector RFC 9535 does not read is an `openapi/invalid-overlay` error
naming the offset where it stops being a query and why, found before any action applies; one using
`~` says so, and one naming a member after a dot by a name RFC 9535's shorthand does not admit,
which holds only letters, digits, underscores and characters past ASCII and starts with no digit,
says what to write, `['x-internal']` for `.x-internal`. `match` and `search` read I-Regexp (RFC
9485) and run on Go's regexp engine, whose time is linear in its input times its program; a pattern
past 4,096 bytes or 16,384 instructions, or that is no I-Regexp, matches nothing, as RFC 9535
section 2.4.6 has it. RFC 9485's grammar admits `^` and `$` as ordinary characters (section 3,
`NormalChar = ( %x00-27 / ... / %x5E-7A ; '^'-'z' / ... )`), and its section 4 has an
implementation "yield Boolean results as specified in [XSD-2]"; its mapping to RE2, Go's dialect,
leaves both as written (section 5.4), where RE2 reads them as anchors, and of that mapping section 5
says "The material in this section is not normative". The compiler follows the mapping, as the
JSONPath compliance suite it is held to does: its cases `functions, match, explicit caret` and
`functions, match, explicit dollar` pass only so. So `^` and `$` anchor, `[\^]` and `[$]` write
the characters, and `\$` is no I-Regexp escape. A pattern written as a literal that matches no
string, one with `$` before a character it must match, or `^` after one, in each top-level branch,
or one that is no I-Regexp, is an `openapi/overlay-action` warning naming why. Any other literal
pattern writing `^` or `$` outside a class, in a group or not, is an
`openapi/overlay-anchored-pattern` info at the action saying each is read as an anchor and naming
the class that writes the character, so `search(@, '$')`, which selects every string, says so.
The selectors of one
application spend one budget of `overlay.MaxSteps` steps: a step for each node a selector visits,
compares or returns, each member a mapping's merges expand to and each element two compared arrays
pair; a step per 8 bytes of a scalar read, once an evaluation, and per 64 bytes two scalars compare;
a match or search its input's length and one more, times its program's weight; and a step for each
link of a target's path the action locates and each member of a mapping it indexes to find one. An
action whose selectors pass the budget, or whose evaluation holds more than `jsonpath.MaxResults`
nodes in its nodelists at once, is an `openapi/budget-exceeded` error and nothing more applies.
A step is a count, not a time: the costliest take some hundreds of nanoseconds, so the budget admits
tens of seconds of selection over a hostile overlay, which only the caller's context cuts shorter.

The actions apply in order, each to the tree the ones before it left. An action takes the first of
`remove`, `update` and `copy` it sets, and a field the specification gives no effect beside a
remove, or an action setting none, is an `openapi/overlay-action` warning. An action setting both
`update` and `copy`, each of which the specification gives no effect beside the other, is refused as
an `openapi/overlay-failed` error naming both. A remove deletes each node its target selects from
the object or array holding it, with every earlier copy of a key the object writes twice, which
would otherwise stand in its place, at Overlay 1.0 a primitive too, since 1.0 has no other spelling for
removing a primitive member. A YAML alias elsewhere naming a node it removed, or one an action
copied in its place, keeps the value it stood for, as a copy the source wrote; a copy that
would grow the tree past `MaxSourceNodes` refuses the overlay as an `openapi/budget-exceeded` error,
and one past the attribution walk's node budget as an `openapi/overlay-failed` error. An update
writes its value into each node its target selects, and a copy writes the one node its `copy`
selects, as that node stood when the action began:

1. into an object, an object is merged member by member: a member the object lacks is added, a
   primitive replaces a primitive, an array is concatenated with an array, an object is merged into
   an object, and any other pair of values is refused;
2. into an array, an array is concatenated with it, and anything else is appended to it as one
   element. Overlay 1.0's text says an update to an array is "an entry to append", which the
   compiler reads as 1.1 does. The two readings give an author different values only where the
   array's elements are themselves arrays, as in an `examples` list: `[[1, 2]]` updated with
   `[3, 4]` becomes `[[1, 2], 3, 4]`, where the literal 1.0 reading gives `[[1, 2], [3, 4]]`, which
   an update of `[[3, 4]]` writes. An array whose elements are not arrays takes no array as one
   entry, so 1.0 gives the update no other meaning there;
3. a primitive replaces a primitive, which only an Overlay 1.1 target selects. An Overlay 1.0
   update selecting one is refused naming both of its spellings: an update of the object holding it,
   or the overlay declaring 1.1.

Any other write is refused, and so are an Overlay 1.1 or 1.2 update or copy whose targets are not
all of one kind, as the specification requires, a copy whose `copy` selects more than one node, and a
remove of the document's root. Each is an `openapi/overlay-failed` error naming the action, at it in
the overlay, and the source compiles to no Document. A target is a position of the value, its
normalized path: one selected twice is written once, and what is written holds no alias, a copy
being taken of what each alias names. A primitive an action writes over one the source declares
where it stands, rather than adding a value the source lacks, is an `openapi/overlay-replaced-value`
info naming both values, at the replacing value in the overlay; info, since overriding is a
legitimate use, and `--fail-on` is a threshold over every code, so a run failing on this one alone
needs a rating by code. A write over what an earlier action wrote is not reported, nor is one into
a copy of a shared value standing at an alias's or a merge's position, nor an array an action
appends to, which keeps what the source declares in it. An action that changes nothing is an
`openapi/overlay-action` warning. An action whose target, or whose `copy`, selects nothing changes
nothing, as the specification has it. Application is strict unless `Overlay.Lax` is set, and under
strict such an action refuses the overlay as a likely typo, an `openapi/overlay-failed` error at
each, once every action has applied; under lax it is an `openapi/overlay-action` warning, and the
rest of the overlay applies.

An action writes the positions it selects and no other. Every target is selected before anything
is written, and each is then made the action's own along its path from the root: a slot holding a
YAML alias, or a node an alias names, is replaced by a copy of the node's content, its anchors
dropped and the aliases inside it kept, and a mapping a member of which a `<<` merge key brings in,
or that is written or removed from, has its merge keys replaced by a copy of each member they bring
in that it does not write, in the value's order; a merge key reaching what the source refuses, a
merge naming no mapping or a key that is no scalar in a mapping it brings in, stays beside the
copies, so the refusal stands. So an update of an anchored schema leaves each
alias of it, and each mapping merging it, the value the source wrote, and a write through an alias
or a merge writes that position alone; none draws a finding of its own, but one the source draws
inside the node copied is drawn again at the copy, at the same line and column. A copy is the size of the subtree the
source writes, not of the value it stands for, keeps the source as the writer of what the source
wrote, and is charged to `MaxSourceNodes` before it is built.

Each action is charged the nodes it can build before it runs: one whose charge would carry the tree,
counted afresh, past `MaxSourceNodes` is refused as an `openapi/budget-exceeded` error with nothing
it would build built, so it is refused while the tree is past the budget whatever a later action
removes. An action that builds nothing, such as a remove, is applied however large the tree, since
the budget holds the tree the overlay leaves. The running count only grows, a replaced primitive
charged a node it does not add and a remove crediting nothing, so over a tree at the budget's edge
each action that charges a node counts the tree afresh: such an overlay costs its actions times the
tree's nodes, which no budget counts. The overlay becomes a second entry in
`Document.Sources`, and every position it introduced or rewrote names the source as its
`Provenance.Source` and that entry as its `WrittenBy` ([provenance](../ir/provenance.md)), while a
position it left alone keeps the source's line and column; a finding about the overlay document
itself names the overlay as its `Source`. Where the walk that attributes positions to the overlay
runs out of its node budget, the overlay is applied and no position names it as a writer, as with
no overlay, reported as an `openapi/overlay-origin-incomplete` warning.

- Codes: `openapi/invalid-overlay`, `openapi/overlay-failed`, `openapi/overlay-action`,
  `openapi/overlay-replaced-value`, `openapi/overlay-origin-incomplete`, `openapi/budget-exceeded`,
  `openapi/overlay-anchored-pattern`
- Tests: `overlay.TestApply_RejectsAnOverlayItCannotRead`,
  `overlay.TestRead_AppliesTheVersionsTheCompilerKnowsAtAnyPatch`,
  `overlay.TestRead_RefusesWhatTheSpecificationRequiresAndTheOverlayLacks`,
  `overlay.TestRead_RefusesACopyInAnOverlay10Document`, `overlay.TestRead_WarnsOfAFieldWithNoEffect`,
  `overlay.TestRead_RefusesAnActionSettingBothUpdateAndCopy`,
  `overlay.TestApply_RefusesATreeOfTheWrongShape`,
  `overlay.TestApply_RefusesAFieldTheVersionDoesNotDefine`,
  `overlay.TestApply_ReadsEachVersionOfAnOverlayAsItsOwnRulesDo`,
  `overlay.TestApply_AppliesAReusableActionAtTheReferencesTarget`,
  `overlay.TestApply_ReadsTheKeyAReferenceNamesDecoded`,
  `overlay.TestApply_RefusesWhatAReusableActionAndAReferenceMayNotWrite`,
  `overlay.TestReadSelector_ReadsEveryTargetAsRFC9535`,
  `overlay.TestReadSelector_SelectsWithEachKindOfSelector`,
  `overlay.TestReadSelector_SelectsWhatRFC9535sExamplesSelect`,
  `overlay.TestReadSelector_RefusesWhatRFC9535DoesNotRead`,
  `overlay.TestReadSelector_SelectsWhatTheCompilerOnceRefused`,
  `overlay.TestHintFor_TellsAStringFromTheRest`,
  `overlay.TestApply_SelectsByTheValuesAFilterComparesNotByTheirSpelling`,
  `overlay.TestApply_ReadsAMemberNamedByAnEscapeOrALiteral`,
  `overlay.TestApply_RefusesATargetRFC9535DoesNotReadAndSaysWhere`,
  `jsonpath.TestCompliance`, `jsonpath.TestSelect_WeighsAPatternByItsProgram`,
  `jsonpath.TestParse_NamesThePatternsThatMatchNoString`,
  `overlay.TestReadSelector_WarnsOfAPatternThatMatchesNoString`,
  `jsonpath.TestParse_NamesThePatternsWhoseAnchorsReadAsAnchors`,
  `overlay.TestReadSelector_NotesAPatternWhoseAnchorIsReadAsOne`,
  `jsonpath.TestSelect_ChargesEachElementTwoArraysCompare`,
  `jsonpath.TestSelect_ReadsAScalarOnceAndChargesTheBytesItCompares`,
  `jsonpath.TestSelect_ChargesAPatternsProgramForAnEmptyInput`,
  `jsonpath.TestSelect_HoldsEveryLiveNodelistToOneBound`,
  `overlay.TestApply_LocatesEachMemberOfAWideMappingAtAStep`,
  `overlay.TestApply_LocatesDeepTargetsAtAStepAPrefix`,
  `overlay.TestApply_GivesEveryGeneratedSharedValueTheTreeOfItsExpansion`,
  `overlay.TestApply_RemovesANodeAndItsDescendantsAtAnAnchorAsItsExpansionDoes`,
  `overlay.TestApply_RefusesARemoveWhoseHolderWouldCopyPastTheBudget`,
  `overlay.TestApply_RefusesATargetThatCostsMoreThanTheStepsAnOverlayMaySpend`,
  `overlay.TestApply_SpendsOneBudgetOnEverySelectorOfAnApplication`,
  `overlay.TestApply_SpendsStepsInProportionToTheActions`,
  `overlay.TestGrafter_WritesAnUpdateAsTheSpecificationSays`,
  `overlay.TestGrafter_TellsA10PrimitiveRemoveFromAnUpdate`,
  `overlay.TestRemove_TakesTheTargetsOfEachHolderTogether`,
  `overlay.TestGrafter_AppliesActionsInOrderToWhatTheOnesBeforeLeft`,
  `overlay.TestGrafter_ReportsAValueTheSourceDeclaresReplaced`,
  `overlay.TestGrafter_TakesACopyOfTheSourceAsTheActionBegan`,
  `overlay.TestGrafter_WritesOnePositionOfAnAlias`,
  `overlay.TestApply_WritesOnlyThePositionAnUpdateTargets`,
  `overlay.TestApply_GivesTheSharedValueTheSameTreeAsItsExpansion`,
  `overlay.TestApply_LeavesAnAnchorsOtherUsesAsTheSourceWroteThem`,
  `overlay.TestApply_AWriteToAMergedMemberLeavesNoSecondKey`,
  `overlay.TestApply_WritesAMergedMemberAsTheMappingsOwn`,
  `overlay.TestApply_KeepsAMergeTheSourceRefusesWhereItFlattensOne`,
  `overlay.TestApply_RefusesAWriteIntoWhatItCannotMakeItsOwn`,
  `overlay.TestApply_RemovesAtOnePositionOfASharedNode`,
  `overlay.TestApply_WritesEachPositionOfAWildcardOnce`,
  `overlay.TestApply_ReadsEachActionTheValueTheOneBeforeLeft`,
  `overlay.TestApply_RefusesACopyThatWouldGrowTheTreePastItsBudget`,
  `overlay.TestApply_DrawsNoFindingForTheAliasesTheSourceWrites`,
  `overlay.TestApply_RemovesAnAliasAndWritesTheAnchorWithoutAFinding`,
  `overlay.TestApply_WritesAMergeKeyAnUpdateNamesWithoutAFinding`,
  `overlay.TestRepairAliases_CopiesWhatARemoveLeftAnAliasNaming`,
  `overlay.TestRepairAliases_KeepsTheWriterOfWhatItCopies`,
  `overlay.TestApplyWithin_RefusesARepairPastItsBound`,
  `overlay.TestApplyWithin_RefusesARepairPastTheNodeBudget`,
  `load.TestLoad_ATaggedMappingLeftBehindARemovedAnchorIsRefused`,
  `load.TestLoad_ReadsANamedOverlayThroughTheReader`,
  `load.TestLoad_ANamedOverlayThatCannotBeReadIsAGoError`,
  `e2e.TestCompile_StrictRefusesAnActionThatMatchesNothing`,
  `e2e.TestCompile_LaxWarnsOfAnActionThatMatchesNothing`,
  `overlay.TestApply_WarnsOfAnActionThatChangesNothingWithoutRefusing`,
  `overlay.TestApplyWithin_DegradesToTheSourceAtTheNodeBudget`,
  `overlay.TestAttribute_AllocatesInTheGraftsSizeNotItsPointers`,
  `e2e.TestCompile_OverlayIsRecordedAsASource`, `load.TestLoad_AnOverlayBuildsWithinTheNodeBudget`,
  `overlay.TestSequence_ChargesAnActionToTheBudgetsEdge`,
  `overlay.TestSequence_ChargesAnActionAfterARemoveOnTheTreeItLeft`,
  `load.TestLoad_AnOverlayDoesNotLiftTheNodeBudgetOfTheSource`,
  `load.TestLoad_APointerAmplificationAnOverlayGraftedNamesTheOverlay`,
  `e2e.TestCompile_AJSONOverlayAppliesAsItsLiteralTwin`,
  `e2e.TestCompile_AnOverlayDescribesOneSchemaOfASharedNode`,
  `e2e.TestCompile_AnOverlayLeavesTheSourceAsItFound`,
  `e2e.TestCompile_AnOverlay12AppliesAndNamesItsSelf`,
  `e2e.TestCompile_AnOverlayFieldTheVersionDoesNotDefineRefusesTheCompile`,
  `e2e.TestOverlaySuite_CompliantSets`, `e2e.TestOverlaySuite_ReadsEachDocumentAsItsVersionDoes`,
  `e2e.TestOverlaySuite_TheReusableActionExampleOfOverlay12`
- Status: built. Limits: a target the Overlay test suite's schema accepts and RFC 9535 does not, such
  as a member name holding `-` after a dot, is refused; an `extends` is compared with nothing; a
  pattern Go's engine cannot compile, such as a repetition past 1,000, matches nothing; `^` and `$`
  in a pattern anchor, where RFC 9485's grammar reads them as characters, which an info says;
  nested descendant segments hold path links in proportion to their steps, which the nodelist bound
  does not count, so an overlay that deepens a chain by copies and selects down it can hold
  gigabytes (#1960); an overlay over a tree at the node budget's edge counts the tree at each action
  (#1959); a finding inside a shared node an action copies is repeated at each copy (#1970).
  Rating
  `openapi/overlay-replaced-value` apart from other codes, to fail a run on it alone, is #1812.

### `validation`

The load phase holds the source to a table of the OpenAPI Specification's rules, at the minor it
declares, and the parser's own validation does not run. Each row of the table cites the sentence of
the specification it comes from. It gives every object's fields, the shape, values and syntax of
each, the empty string held to a syntax as any text is, so `''` breaks an email's or an absolute
URI's and not a URI reference's, which fields each minor requires, the rules that bind fields together, and the defaults of
fields left unwritten that a lowering reads ([`multipart-encoding`](#multipart-encoding)); what data
cannot state is written as code beside it, such as a path template against its path parameters, a
server URL against its variables, a tag's parent, where an encoding applies, and whether a 3.0
`nullable` has a `type` beside it to apply to ([`nullable`](#nullable)). Each finding is reported
as `openapi/validation` suffixed with its class (`openapi/validation/validation-required-field`), at
the node it is about, its message naming the object, the field, the minor and the section of the
specification it cites. A finding never refuses the document,
which lowers as written.

A finding is an error where the document's meaning is missing or contradicted: a REQUIRED field
absent, a value of the wrong type, a value outside the set the specification allows, fields that
exclude each other written together, a path template and its path parameters disagreeing, a list or
map holding fewer entries than the specification requires or repeating one it requires to be unique,
as JSON Schema requires of `allOf` and of `required`, or a value a grammar the specification cites
refuses, such as a token or an anchor's name, a headers map's key, which is a header's name, among
them ([`header-style`](#header-style)). Every mapping, values and extensions included, is held
to the Format section's rule that a key is a string, so a key that is no scalar, which the parser
reads as the empty string ([`non-scalar-key`](../parser-library.md#non-scalar-key)), is an error,
reported at the mapping's pointer and the key's own line and column, and its entry is read as left
out by every reader, the model among them, since `""` would be one pointer for every such key,
and so is a `<<` merge key naming neither a mapping nor a list of mappings, from which the parser
merges nothing ([`merge-keys`](../parser-library.md#merge-keys)). It is a warning where the document
breaks a rule of its version and still says what it means: a key outside the pattern the
specification gives, a key a mapping writes twice, a number or a boolean where a string belongs, a
field written where the specification gives it no effect (`validation-no-effect`), or a `null`
written for an optional field, which is read as the field left out, by the rules that bind it to
other fields as well. Every keyword of a Schema Object is such a field, since the schema lowering
reads its `null` as left out too ([`null-keywords`](#null-keywords)), a numeric bound included. A
`null` written
as a schema entry, which is another object's `schema`, a component, or an entry of a map or a list
of schemas, is an error, since that lowering reads it as the empty schema, a value, and a list
counts it among its entries. One written for a field
whose requirement another rule reports is left to that rule.
A security requirement's name is no field to leave out: its `null` lists no scopes, and the name
stands ([`security-requirements`](#security-requirements)).

The source is read as it writes it, an applied overlay's patch included, each object as the kind
its position gives it. Each Schema Object is held to the rows of the dialect it is read in
([`dialect-keywords`](#dialect-keywords)), what it holds and what only that dialect places, such as
a draft's `definitions`, included: a keyword a draft constrains otherwise, such as draft 4's
`required`, which holds an entry, is held to the draft's constraint, and a keyword only the draft
defines to its row, a number where a string belongs being the warning it is anywhere. Those rows are
known once the bindings are read, so a document that may name a dialect, a 3.1 or 3.2 one writing
`jsonSchemaDialect` or the key `$schema` anywhere in its tree, has its Schema Objects judged then,
their findings after its others; any other document is judged whole at once, with its minor's own
rows. An object a `$ref` reaches that the source's own reading did not take as that kind, one under
an `x-` key or in another document, is read as the kind the reference expects and held to the minor
of the document that writes it
([`openapi-versions`](#openapi-versions)). A finding about it is placed at its node, in the entry of
`Document.Sources` for the document that writes that node, a file the source spells two ways being
one document. A key a mapping of such a document writes twice, anywhere in it, values
included, is a warning at the repeat, in its entry, read over the whole document once: the
compiler reads only the last copy, whichever copy a reference names. One a reached object's check
reports at the same place keeps that check's wording. Only this rule is read over the whole of
another document: a key that is no scalar there is reported only where a reference reaches it, so
a shared file's unread content fails no compile. One about a node of a document no entry lists goes to the least `$ref` reaching it,
its message naming where in that document it lies. Each finding is made once: a node an alias or a
merge key mounts at several positions draws one, naming the least position in that document that
read it, by the order below, where that is another place than the one it is placed at. A node aliases share is placed at the least pointer of the
places that mount it, where its anchor is written and where each alias of it is, pointers compared
token by token and each token by its bytes, so `/A/…` comes before `/A-/…`; its line and column
stay where its text is written. A message naming another node, as a repeated entry's names the copy
it repeats, names the pointer a finding on that node is placed at, or, where no pointer places it,
the position that read it. Which place a document writes first decides nothing, and a document
whose aliases lead back into what holds them keeps every written place: it is refused, at the
cycle.

A value the table refuses as the wrong type is read as the field left out by every reader of the
source, wherever the table reads it, the parser's model among them, and so is one of what a `$ref`
reaches outside the source's own reading, raw YAML under an `x-` key or another document, which is
judged before anything is built for it ([`references`](#references)); a construct kept verbatim shows
it as the source wrote it. It is read so once every judgment of the document is in: in a document
that may name a dialect, once its Schema Objects are judged, when the model and the `$ref`s are read
again, so such a document reads a refused value as the same document naming none does. Such a value is one with none of the shapes its field admits, a boolean
written as a string among them, which the model reads as `true` or `false`
([`string-as-boolean`](../parser-library.md#string-as-boolean)), a scalar or a list under a tag
outside YAML 1.2's JSON schema ruleset, which OpenAPI limits tags to and which the table reports
naming the tag, and a value that is no mapping where an object belongs; a `null` is read so
already. A field that takes any value, a default or an example, takes a tagged value as written. A
node an alias, an anchor or a `<<` merge key shares is read so only where every place the table
reads it refuses it: one place that accepts it keeps it as written for all, and a place the table
does not read, such as an `x-` key, or one that takes any value, such as a default, an example, a
`const` or an `enum` member, has no say.

A rule another lowering reports is stated in the table and left to it, so a document draws one
report: [`operation-ids`](#operation-ids), a repeated parameter ([`parameters`](#parameters)), a
security requirement naming no scheme and a scheme naming no mechanism
([`security-requirements`](#security-requirements)), an array schema without `items` in 3.0
([`missing-array-items`](#missing-array-items)), and a request body's empty `content`
([`request-body`](#request-body)). A numeric bound is judged as the schema lowering reads it, the
decimal its text spells, so one no decimal spells is [`constraints`](#constraints)' error rather
than a value of the wrong shape. A construct a later minor added is
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
  `oas.TestCheck_TheEmptyStringIsJudgedByItsFormat`,
  `oas.TestTable_EveryRowCitesTheSpecification`, `load.TestJudgeSource_PlacesEachFindingAtItsNode`,
  `load.TestJudgeSource_RefusesTheAnchorsNoReferenceCanName`,
  `load.TestLoad_WhatAnAliasMountsIsReportedOnce`,
  `load.TestResolve_AnObjectReachedTwiceIsReportedOnce`,
  `load.TestResolve_AFileSpelledTwoWaysPlacesAFindingOnce`,
  `load.TestResolve_AReachedFindingNamesThePositionThatReadIt`,
  `e2e.TestCompile_AFindingInAnExternalDocumentKeepsItsRule`,
  `canary.TestValidationOracle_AgreesWithTheLibrary`,
  `oas.TestCheck_ASchemaIsHeldToItsDialectsRows`, `oas.TestCheck_AFindingInADraftNamesItsDialect`,
  `oas.TestCheck_ARootReadsItsOwnRows`, `oas.TestCheckPending_ReadsWhatACheckHandedBack`,
  `load.TestNamesDialect_ReadsTheKeyAndTheField`, `load.TestJudgeSource_DeferringASchemaChangesNoFinding`,
  `e2e.TestOlderDrafts_ASchemaIsHeldToItsDialectsRows`,
  `oas.TestCheck_JudgedTellsEachValueTheCheckRefusedWhereItIsWritten`,
  `oas.TestCheck_ATagOutsideTheRulesetIsRefusedWithTheTagSaid`,
  `oas.TestCheck_JudgedTellsEachValueAFrameSharesAcceptedOrRefused`,
  `nodeview.TestUnread_ReadsAsNullAndWrittenReturnsWhatTheSourceWrote`,
  `e2e.TestCompile_ABooleanWrittenAsAStringIsReadAsTheFieldLeftOut`,
  `e2e.TestCompile_AScalarOrSequenceTaggedOutsideTheRulesetIsReadAsLeftOut`,
  `e2e.TestCompile_ANodeASchemaAndAPropertiesMapReadStaysAsWritten`,
  `e2e.TestCompile_ASharedNodeIsReadAsItsInlineSpellingIs`,
  `oas.TestCheck_AFieldOfAnyShapeAcceptsNothing`,
  `e2e.TestCompile_AScalarAValueSharesIsReadAsTheRefusingPlaceReadsIt`,
  `e2e.TestCompile_ARefusedValueIsReadAlikeWhetherADocumentMayNameADialect`,
  `e2e.TestCompile_ANodeASchemaAcceptsIsKeptInADocumentThatMayNameADialect`,
  `oas.TestCheckFormat_PlacesEachKeyAtItsOwnLine`, `nodeview.TestKeyOf_AKeyThatIsNoScalarNamesNoKey`,
  `e2e.TestCompile_AKeyThatIsNoScalarIsReportedOnceAndLeftOut`,
  `e2e.TestCompile_TwoKeysThatAreNoScalarAreEachReportedInEitherOrder`,
  `e2e.TestCompile_AKeyWrittenTwiceInAnotherDocumentIsReported`,
  `e2e.TestCompile_AKeyWrittenTwiceInsideAReachedObjectIsReportedOnce`,
  `e2e.TestCompile_AKeyThatIsNoScalarInContentNoReferenceReachesIsNotReported`,
  `e2e.TestCompile_AFindingOnANodeAliasesShareIsPlacedAtItsLeastPointer`,
  `e2e.TestCompile_AnInnerSharedNodeIsPlacedAtItsLeastPointerInEveryDeclarationOrder`,
  `e2e.TestCompile_ATypeMismatchOnASharedMountIsPlacedAtTheLeastMount`,
  `e2e.TestCompile_ANodeInsideTwoMountsIsPlacedByTheirTokens`,
  `oas.TestCheck_AFindingNamesTheLeastPositionThatReadIt`, `oas.TestLessPointer_OrdersTokenByToken`,
  `oas.TestCheck_ARepeatedEntryNamesItsFirstCopyWhereItIsPlaced`,
  `e2e.TestCompile_ARepeatedEntryNamesItsFirstCopyWhereItsDocumentHoldsIt`,
  `scan.TestLeast_OrdersByThePointersTokens`, `scan.TestLeast_AgreesWithSortingEveryPointer`,
  `e2e.TestCompile_AValueARefLandsOnIsJudgedBeforeItIsBuilt`,
  `e2e.TestCompile_AValueAnotherDocumentsRefLandsOnIsJudgedBeforeItIsBuilt`,
  `e2e.TestCompile_ANullableARefLandsOnWritesWrongIsNotAdmitted`,
  `e2e.TestCompile_AValueOneReadingRefusesAndAnotherAcceptsIsKeptForTheOther`,
  `load.TestLoad_WhatAReferenceReachesIsJudgedOnceBeforeItIsBuilt`
- Status: partly built: where the table refuses an `$id`'s value in a document that may name a
  dialect, a schema below it is judged in the dialect that `$id` roots as written (#1958).

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
warned of as a later minor's construct as well, whatever it is, so no row of the table names them;
a keyword 3.1 added draws that warning alone there. JSON Schema drafts 4, 6 and 7 ignore a schema's
keywords beside its `$ref` too, and 2019-09 applies them, so in a schema read in one of those drafts
([`dialect-keywords`](#dialect-keywords)) the schema lowering applies each as 2020-12 does and the
table warns of each field the draft's rows define at its key, OpenAPI's own among them, bar those
whose reading applying them leaves alike: the `$schema` naming the draft, `definitions`, whose
schemas only a `$ref` reads, and `$comment`. The identifier is not applied, as applying it would
change what the `$ref` names: it names nothing there, as the draft has it
([`references`](#references)), and draws no warning. A key the rows define nothing for, an extension
or an unknown keyword, is kept as written either way, and draws none, bar a keyword 2020-12 added,
which draws its own warning as anywhere in the draft's schema.
An object a `$ref` reaches is judged as the source's own are, wherever it is written, and reported
where [`validation`](#validation) places a finding about it. A
Path Item's `additionalOperations` before 3.2, and a field 3.2 added to a Discriminator or XML
Object, draw the warning as any other construct does. A keyword JSON Schema 2020-12 added, written
in a 3.0 schema or in one a draft before it reads ([`dialect-keywords`](#dialect-keywords)), is read
as 2020-12 reads it and draws the warning too, at its key, its value held to 2020-12's row, true and
false standing as schemas where that row admits a schema, an entry of its list or map among them: in
a 3.0 schema in the words above, as a field 3.1 added, and in a draft's as a keyword its dialect
does not define. The rules 3.1 states over such a keyword, such as `prefixItems` holding an entry,
hold in a draft's schema and not in a 3.0 one, where the declared minor's rules run. Three such
keywords are not read: a `$schema` in a 3.0 schema, whose dialect is fixed, which the warning says
is kept verbatim and chooses nothing; draft 4's `$id`, which its `id` spells, read for nothing and
warned of by no such warning; and a `prefixItems` beside the `items` list a draft reads as its own
tuple, which is kept verbatim beside it ([`tuples`](#tuples)), as the warning says. The same table
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
  `load.TestResolve_AReachedObjectIsHeldToTheLaterVersionTable`,
  `oas.TestCheck_AKeywordALaterDialectAddsIsWarnedOf`, `oas.TestCheck_ARefsSiblingsInADraftAreWarnedOf`,
  `oas.TestCheck_ABooleanSchemaStandsWhereItsDialectAdmitsOne`,
  `oas.TestSchemaRules_LaterKeywordsAreTheOwnRowsADialectLacks`

### `references`

A `$ref` resolves within the source, as a JSON pointer. One naming another document is read through
the reader the caller supplies (`compilers.Options.Reader`), each file or URL read and charged once
however many references name it, by every spelling naming that one document (below), and held to the
source's budgets and refusals ([`source-budgets`](#source-budgets)), where a document past one fails
each reference naming it. Its document part is read against the document writing it, by RFC 3986
§5.2, and the source's against where the source lies: the absolute path `compilers.Source.Location`
names, which the engine sets with the symbolic links of the spec's directory resolved, or the
source's `Path` where it is unset. Every file a compile names at an absolute path, the source
included, lies where the reader places its directory (`compilers.DirectoryLocator`), its links
resolved, so a file reached through a link to its directory and through the directory itself is one
document. The working directory is never read, so what a reference names depends on no spelling of
it, nor on how the spec was named, and a relative path names the file an absolute one does only
where the source lies at an absolute path. A file's part is percent-decoded,
so `my%20file.yaml` names the file `my file.yaml`, unless decoding would read it otherwise, as an
escaped separator or `#` would, where it is read as written; and a `file:` URI names the file at its
path, as a reader reads it, so a file is one document however a reference spells it. A document's
label is its location from the directory its source lies in, whatever spelling names it; an absolute
reference changes no label. A URL of any other scheme is spelled as RFC 3986 §6.2.2 normalizes it,
and an `http` or `https` one as §6.2.3 does too, where it is located and where an identifier declares
it: its scheme and host in lower case, each escape of an unreserved character decoded and the hex of
every other in upper case before its dot segments are removed, and for `http` and `https` an empty
path made `/` and an empty or default port dropped, while an empty query's `?` and a host's final dot
stay. So every spelling of one URL is one request, one document and one `Document.Sources` entry,
labelled the one way. Against a document fetched over http(s), a part naming no scheme, an
absolute path and a network-path reference among them, names what the document's URL makes of it, on
the network. A reference written in a document read over http(s), the source included, is read
only from an http(s) URL: one naming another location, by a `file:` URI, a drive path, or a base an
`$id`, a draft 4 `id` or the source's `$self` moves onto one, fails unread where it is written, and
the rest of the document is read. Nor does such a reference reach an address more private than the
one its document was read from, a public address ranking below a private one and a private one below
loopback: the reader bounds the read to that space (`compilers.DocumentRequest.Reach`), judging each
address it dials, redirects included. Each read states two spaces: where the address that answered
lies (`compilers.DocumentRead.Space`), which ranks the document as a referrer, and the most private
it can have dialled, every hop of a redirect included (`compilers.DocumentRead.MostPrivate`), which
ranks it as a target. A document
read before is judged by the second, and where that read stated none, by a read of the reference's
own at its reach, made once per reach, which reads nothing into the compile and charges nothing to
the document budget. A read whose reader states no bound, or one the reader cannot bound, is refused
to such a reference too. What the document's own references reach is the space its first read
stated, public where it stated none. Each refusal is that reference's alone, worded for its reach:
another naming the document, from a file or the source, reads it. A source located at a URL is
bounded as a public document. Through an HTTP proxy the host is judged instead: an address literal
as the address the proxy dials, a name by every address this machine resolves it to, and a document
read through one by a name, or redirected through one, states no space, so its own references reach
as a public document's do.
The rule leaves four things unbounded, each in the Status below: what a proxy dials after resolving
a name again; the base a redirected document's references resolve against, which stays the location
asked while its space is the address that answered; the time a compile's reads take together, which
no deadline bounds; and a name resolving to addresses in several spaces, read wherever the dialler
connects first, so a document's bound turns on which asker read it first. The read stays inside the
directory the source lies in, symbolic
links included, and follows relative references alone, unless `Options.AllowExternalRefs` lets it
reach any file and http(s) URLs: a `$ref` naming a document other than the source by a `file:` URI
or by an absolute path fails unread, whatever another `$ref` names that document by. Each is judged
by its own spelling, so what the default reads depends on no order references are read in. In either
mode a `$ref` naming a URL whose scheme is none of `http`, `https` and `file` fails unread, where a
reader would take it for a path; a file whose name holds a colon is written `./a:b.yaml`, and is
read as that file. The caller may map a URI prefix to a location, a directory or an http(s) URL
(`Options.Locations`, `--opt locate=<uri-prefix>=<location>`); nothing is mapped by inference. A
prefix that is no absolute URI ending in `/`, or holds a query or a fragment, a prefix mapped twice,
and a URL location ending in no `/` are refused as an option error. The
mapping is applied where a document a URL names is opened, once the URL is spelled one way, before
any read judges it, and never to an identifier: an `$id` or a `$self` keeps the URI it writes, so a
reference relative to one is resolved against that URI, and the URL it names is then mapped or, out
of every prefix, read as a URL. The longest prefix wins: the rest of the URL is joined to a directory segment by segment,
each decoded, and one decoding to a separator names no location, so the reference fails unread and
no spelling leaves the location; a URL location takes the rest as written. The document is then
read, confined, named and labelled where its bytes are, so a file reached by its path and by a
mapped URL is one document, and a mapped location a document read over http(s) may not reach is
refused as any other. A URL refused as on the network says a mapping would place it. A document outside fails each reference naming it, and with no reader no document
is read.

Each document a reference reaches is a `Document.Sources` entry after the source and its overlay,
ordered by its location: its path from the directory the source lies in, or where no relative path
reaches it, as from a source lying at a relative path to an absolute one, its own path. Its entry's
path is that location joined to the directory of the source's `Path`, so a spec named relatively
lists the documents beside it relatively, the `..` segments climbing out of that directory kept, as a
link may place it elsewhere than cleaning them away would. What it declares lowers there, as
the source's own lowers where the source declares it: a schema a `$ref` lands on, and a parameter,
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
wherever a walk meets it, and it is bound as every other kind is, so it is read under the same
reader, budgets and refusals, a chain of entries is followed as any chain is, and what one reaches
is held to the table ([`validation`](#validation)). What it names lowers once, at the pointer
declaring it, however many content entries reach it, with the references it holds resolved where it
is declared, and a `$ref` to an inline content entry reaches it, as one to an inline parameter does.

A Reference Object is bound by the compiler, in the table a schema `$ref` is bound in and by the
same reading, never by the parser's resolver, so its answer depends on nothing read before it. Its
document part names a document as a schema `$ref`'s does, against the document writing it, which no
`$id` rebases. Its fragment is empty, naming that document's root, or a JSON pointer read from the
root as RFC 6901 reads one, a token holding any character: a pointer ending in `/` names a member
whose key is empty, and a plain name, or a fragment holding a second `#`, names no object. A mapping
at a reference object's position is a reference where it holds a `$ref` that names something
(below), as the document's JSON spells it, so a `$ref` a `<<` merges in makes one. Nothing written beside its `$ref` is read, a
path item's fields aside ([`path-item-ref-fields`](#path-item-ref-fields)), as OpenAPI says "any
properties added SHALL be ignored": a `$ref` there is no reference and draws no finding. It lands on
an object of its own kind: a position the source's grammar reads as another kind, a schema, a map of
objects or a value holds none, and raw YAML, as an extension's value or any position of another
document, is read as the kind it is reached as where it is a mapping. It is held to the table
before anything is built for it, by the one judgement whose findings are reported, so a value that
judgement refuses reads as left out there too, and a shape the model faults on is set aside there
as in the source. An object is built there, and the references inside it are bound where it lies,
as the source's are; one no object can be built from is reported as an `openapi/validation` error
at the reference, and lowers nothing.

A schema `$ref`, and a discriminator mapping value naming no schema component of the entry document
([`discriminator`](#discriminator)), is bound by the compiler itself, never by the parser's resolver
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
it `id`, its `$id` then naming nothing. In drafts 4, 6 and 7 one naming a fragment alone, `#name`,
declares that plain name as an `$anchor` does, which is why the rule table holds an `$id`'s format
to the row of the schema's dialect ([`validation`](#validation)); one beside a `$ref` gives it no
base, roots no resource and declares no name, as those drafts ignore every keyword there;
`definitions` holds schemas as `$defs` does, and so does `dependencies` where a value is one. A
schema another holds roots a resource where it writes an identifier as the
dialect embedding it spells one, as 2020-12 has an embedded resource do (§9.3.1), or as the dialect
its own `$schema` names does, unless it writes a `$ref` whose siblings the dialect embedding it
ignores. An `$id` names the URI its part before the `#` resolves to against the base above it, by
RFC 3986 where that base is a URI and as a `$ref`'s document is placed where it is a file's path,
and identifiers match as a located URL is spelled (above). One with
a fragment that is not empty names none, as 2020-12 admits none, one naming the URI of the resource
holding it starts no resource, and one naming the source, however spelled, returns to the
document's. A declaration answers a reference only where the referring document could read the
declaring one: a document answers itself, a document read over http(s) answers another only for a
URI of its own origin (scheme, host and port), and a document read over http(s) is answered by no
file, the source among them; for any other asker it declares nothing, and the URI is a location to
it. A URI no schema declares names the document at that URI, read as any other is: one read
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
it does from the pass that would read against what an earlier one read. A pass reads every
document a reference names for the first time once its walks are done, in two tiers: the files,
which cost no request, then, once what every document open declares is read, each URL no document
open declares to the document that asked for it. A document is reached by the reference that
resolves into it, not by its read, and a reference whose URI a document read and not yet reached
declares waits for that document to be reached; where nothing more is reached, the documents
declaring what waits are reached, and what still waits is read as a location. The pass goes on,
reading again only the references that waited, while what the documents it reached declare changes
no URI whose answer it kept. So a chain of documents, each declaring the URI the one before it asked
for, is read in one pass, its cost growing with its length, and a file declaring a URL answers it
with no request. A pass is read again only where a location's own content leads to a declaration of
its URI; a URL held beside the URL-named document declaring it is read for nothing. A pointer passing a reference object, with tokens left to read, reads on where that
reference's chain ends, in whichever document, save at a path item, where a token naming a field
written beside its `$ref` reads that field ([`path-item-ref-fields`](#path-item-ref-fields)); a
pointer whose read needs a chain that needs that read is unresolved. A pointer into another OpenAPI
document passes the reference objects its grammar places, as one into the source does; a document
with no `openapi` member has no grammar to place one, so a pointer into it passes none. A `$ref`
written in another document's content is read in that document. What a `$ref` lands on lowers where it is declared, at
the position its pointer reaches read through each reference object it passes, in whichever
document, so two spellings reaching one declaration name one node. A use site takes what its schema
`$ref`'s chain ends on — an annotation, a form body's parts, a file body or a discriminator —
whichever documents the chain passes, as its type names that schema. An empty `$ref` names the
document's root, which holds no schema, so a chain reaching one ends on none and lends nothing, as
any chain that fails does, though the schema writing it keeps what it declares beside it. A YAML
alias or merge key is a copy written where it is placed, so what it holds is read at that position,
as the document's JSON places it, and raw YAML a schema `$ref` lands on, under an extension or in
another document, is read as a schema, as is a schema beneath an Encoding Object that another nests
through 3.2's `encoding`, `prefixEncoding` or `itemEncoding`, which no model holds. Every reading
of what the model holds takes such a schema as raw YAML under an extension at its place, the
`#/$defs/...` rule's and the weight of a copy ([`source-budgets`](#source-budgets)) among them,
but a pointer read on past a reference object reads afresh: a schema below the object its chain
ends on is that object's, which the load phase builds where no model holds one, as under an
extension. A `$ref` written beneath such a nested Encoding Object, a schema's or a Reference
Object's, is bound and reported as one anywhere else is, though the object itself is kept verbatim
([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)). A schema position of the model holds the
schema the parser built there, whatever its YAML, so a `$ref` to a component written as a string
names that component, as it lowers. A key written twice in one mapping is read as its last
occurrence, raw YAML included, as the parser's model reads one
([`duplicate-key`](../parser-library.md#duplicate-key)).

A reference that resolves to nothing — a pointer naming no node, a position holding no schema, a
component its section does not hold, another document the compile may not read, a plain name or a
URI no schema declares, or two do — is reported as an `openapi/unresolved-ref` error at the
referencing position, with why, and the position lowers without it: a schema position to the top
type, any other position to nothing at all, so the parameter, response or body it would have named
is absent. Each is reported once, where it is written, in whichever document, and a `$ref`, a
schema's or a reference object's, whose chain ends on another failing `$ref` is not reported for it,
as that one is. A reference object whose hop lands on a position holding no object of its kind, such
as a string or a `null` under an extension, draws that error, worded as a schema `$ref`'s is (`it
names a position that holds no parameter`), and nothing is reported at the position, whichever
reference reaches it first.

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
(`nodeview.MergeDepthLimit`) is reported as an `openapi/degraded-construct` warning, and a `$ref`
past it is left unresolved, as is one that reads a key past it: a key its pointer names, an `$id`
that may give the base it is read against, or, for a `#/$defs/...` pointer, a definition the rule
reads before the one that would answer. So is a plain name no schema declares where a schema merging
past the bound may; a URI is read as a location all the same, and a `$dynamicRef` in that document
is not expanded ([`dynamic-ref`](#dynamic-ref)). A schema whose own merges run past the bound is
read from its keys within the bound alone, never half from them and half from what the parser
expands: a keyword merged in past the bound is neither lowered nor kept, whichever reader asks, a
request body's reading as a file among them, though not yet the walk of a form body's parts, which
reads what the parser expands. Nor is one its keys list from a source that a source past the bound,
writing the keyword too and coming first, outranks: a key is read only where the parser read the
keyword's value from it, and a keyword the parser holds no field for, whose value it read from no
node to compare, only where the schema writes it itself, which no merge outranks; one left out is
not kept as an unknown key either. Two sources writing equal values are told apart all the same. The
schema draws an `openapi/degraded-construct` warning of its own, at its pointer, once however many
positions lower it; one kept verbatim, as a `not` is, is refused there instead
([`verbatim-preservation`](#verbatim-preservation)). Its extensions, and a key no keyword names, are
read as the parser expands them, and [`unknown-keys`](#unknown-keys) reports the latter. A
`$dynamicRef` naming no `$dynamicAnchor` is the `$ref` it spells, and is read as one in every
respect here, its `openapi/unresolved-ref` worded with the keyword it is written under. A chain of
references that closes on itself is [`reference-cycles`](#reference-cycles)', whose message names
each link a `$ref`, whichever keyword wrote it.

- Cases: `schema-identifiers`, `component-media-types-32`, `reference-object-chains`
- Codes: `openapi/unresolved-ref`, `openapi/budget-exceeded`, `openapi/degraded-construct`,
  `openapi/validation`

A Reference Object whose `$ref` is a null, a list, a mapping or a scalar tagged outside YAML 1.2's
JSON schema ruleset, written by the mapping or merged into it by a `<<` key, names nothing: the
entry it stands in is read as left out, its other keys with it, and the rule table's error at the
`$ref` is the one report of it; a component kept verbatim shows it as written
([`unreferenced-components`](#unreferenced-components)). A `$ref` written as a number or a boolean
is read as the text it spells, as such a value is wherever a string belongs, beside the rule table's
warning ([`validation`](#validation)), so `$ref: 5` names the file `5`. The parser's
model faults building such an object, and a 3.2 `additionalOperations` entry that is no mapping
([`nil-populate-panic`](../parser-library.md#nil-populate-panic)), so the load phase sets each
aside for the model's builds and puts it back before anything else reads the tree, in the source
and in what a reference reaches outside its model alike. A Path Item's
`$ref` is the item's own field, read the same way: one that names nothing is read as left out, and
the item as what else it writes.
- Tests: `e2e.TestCompile_ExternalFileRefIsNotReadByDefault`,
  `e2e.TestCompile_AFetchedDocumentMayNotReachAMorePrivateAddress`,
  `e2e.TestCompile_AnAddressOneDocumentCannotReachIsStillReadForAnother`,
  `e2e.TestCompile_APrivateDocumentMayReachAPrivateAddressButNotALocalOne`,
  `e2e.TestCompile_AReaderThatCannotBoundAReadFailsOnlyTheAsker`,
  `e2e.TestCompile_ASourceLocatedAtAURLIsBoundedAsAPublicDocument`,
  `e2e.TestCompile_AFetchedDocumentWhoseReaderSaidNothingIsRefusedToABoundedAsker`,
  `e2e.TestCompile_ADeclarationOfAFileAnswersNoFetchedDocument`,
  `bind.TestBind_AChainOfDeclaringDocumentsCostsWorkLinearInItsLength`,
  `bind.TestBind_AChainOfDeclaringURLsCostsWorkLinearAndEachIsRequestedOnce`,
  `bind.TestBind_RequestsWhatMainRequests`,
  `e2e.TestCompile_OneURLSpelledSixWaysIsOneRequestOneKeyAndOneType`,
  `e2e.TestCompile_ADocumentAnotherOriginServesDeclaresNothingForTheSource`,
  `e2e.TestCompile_AFilesDeclarationAnswersTheSourceAndNoRead`,
  `e2e.TestCompile_AFetchedDocumentReadsAURLAFileDeclares`,
  `e2e.TestCompile_ATwoDocumentsDeclaringOneSelfAnswerNoReference`,
  `e2e.TestCompile_AFileDeclaringAURLIsNotRequestedWhereReadsAreAllowed`,
  `bind.TestBind_ADeclarationAnswersOnlyWhoCouldReadItsDocument`, `bind.TestBind_ASelfIsADocumentsBaseAndName`,
  `bind.TestBind_AMappedLocationIsJudgedWhereAFetchedDocumentReadsIt`,
  `e2e.TestCompile_AFileIsOneDocumentWhateverItsReferenceSpellsIt`,
  `e2e.TestCompile_AMappedURLIsTheDocumentItsLocationHolds`,
  `e2e.TestCompile_AMappedURLThatNormalizesOutOfItsPrefixIsRefused`,
  `e2e.TestCompile_AMappedSegmentThatDecodesToASeparatorNamesNothing`,
  `e2e.TestCompile_ALocationWithNoFinalSeparatorIsTheDirectoryItNames`,
  `e2e.TestCompile_AFetchedDocumentsReferenceToAMappedURLIsNotAFile`,
  `load.TestMapped_PutsAURLUnderTheLongestPrefix`, `load.TestLoad_AMappedURLNamingNoLocationIsRefusedUnread`,
  `openapi.TestDecodeOptions_LocateMapsEachPrefixToItsLocation`,
  `openapi.TestDecodeOptions_LocateRefusesAPrefixThatIsNoURIPrefix`,
  `e2e.TestCompile_AFileURIIsRefusedByItsSpellingWhereReadsAreConfined`,
  `e2e.TestCompile_AnIdentifierAndAReferenceMeetInOneSpelling`,
  `resolve.TestLocate_AnAbsoluteURLIsSpelledByRFC3986Normalization`,
  `resolve.TestLocate_NormalizingTwiceChangesNothing`, `bind.TestNormalURI_IsIdempotentOverGeneratedURLs`,
  `bind.TestNormalURI_SpellsEachEqualURLOneWay`, `bind.TestNormalURI_KeepsWhatEachSchemeWrites`,
  `e2e.TestOlderDrafts_AnIDNamesAResource`, `e2e.TestOlderDrafts_AnIdentifierBesideARefSetsNoBase`,
  `e2e.TestOlderDrafts_AnAnchorTakesItsDraftsGrammar`,
  `bind.TestDialectRule_AFragmentIdentifierDeclaresByItsDialectsGrammar`,
  `bind.TestFragmentName_DeclaresAnyNameDraft4Writes`,
  `load.TestJudgeSource_RefusesTheAnchorsNoReferenceCanName`,
  `bind.TestDialectRule_AnIdentifierBesideARefNamesNothing`,
  `bind.TestDialectRule_AnAnchorTakesItsDialectsGrammar`,
  `bind.TestDialectRule_ReadsWhatADialectSpellsOtherwise`,
  `bind.TestDialectRule_ANestedSchemaNamesItsDialectAtAResourceRoot`,
  `bind.TestDialectRule_AWalkStartsInTheDialectAboveIt`, `e2e.TestGolden_SplitSpec`,
  `e2e.TestExternalContent_ARefInItNamesItsOwnDocument`,
  `e2e.TestCompile_AResolutionFailureIsReportedOnceAtItsRef`,
  `schema.TestMapping_InAnotherDocumentIsReportedWhereItIsWritten`,
  `lowering.TestInDocument_PlacesWhatAnotherDocumentDeclares`,
  `service.TestDeclared_LowersAReferencedObjectWhereItIsDeclared`,
  `service.TestDeclared_ReadsTheEndOfTheDocumentWritingTheReference`,
  `e2e.TestCompile_ASiblingRefResolvesWithinTheSourcesDirectory`,
  `e2e.TestCompile_ARefOutsideTheSourcesDirectoryIsRefused`,
  `e2e.TestCompile_ASymlinkOutOfTheSourcesDirectoryIsRefused`,
  `e2e.TestCompile_ARefOutsideTheSourcesDirectoryResolvesWhenAllowed`,
  `e2e.TestCompile_ByDefaultOnlyARelativeReferenceIsFollowed`,
  `e2e.TestCompile_ByDefaultEachReferenceIsJudgedByItsOwnSpelling`,
  `e2e.TestCompile_AnAbsolutePathInANetworkDocumentNamesItsHost`,
  `e2e.TestCompile_ANetworkDocumentReadsNoLocalFile`, `resolve.TestLocate`, `resolve.TestFilePart`,
  `resolve.TestFileOf`, `resolve.TestSameDocument`,
  `resolve.TestPlaces_PlacesAFileWhereItsDirectoryLies`, `load.TestLoad_RejectsARelativeLocation`,
  `load.TestLabelOf_IsTheLocationTheSourcesDirectoryReachesADocumentBy`,
  `load.TestShownPathOf_NamesADocumentAsTheHandedPathNamesTheSource`,
  `e2e.TestCompile_ANetworkDocumentReadsNoLocalFileByAnyReference`,
  `e2e.TestCompile_ASourceReadOverTheNetworkReadsNoLocalFile`,
  `load.TestExternal_ASourceReadOverTheNetworkReadsNoFile`, `bind.TestReachable`,
  `load.TestLoad_AFileURIAndAPathAreOneDocument`, `e2e.TestCompile_AFileNamedLikeAURLIsReadAsAFile`,
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
  `load.TestLoad_ARootDefinitionAnswersBeforeASchemasOwn`, `bind.TestBind_ReadsEachHop`,
  `bind.TestBind_PassesAReferenceObjectWhereAPointerIntoAnotherDocumentStarts`,
  `bind.TestBind_ReadsADefinitionByTheDefsRule`,
  `e2e.TestCompile_DefsPointerUnderANestedIDReadsTheBaseItNames`,
  `e2e.TestExternalContent_ARefLendsItsUseSiteWhatItsTypeReaches`,
  `schema.TestSchema_ARefThroughAnEmptyRefLendsNothing`,
  `bind.TestBind_ReadsEachPositionAsTheJSONItSpells`, `bind.TestBind_ReadsARepeatedKeyAsItsLast`,
  `bind.TestBind_StopsAtItsBudget`, `bind.TestBind_ThePassTheBudgetRunsOutInStands`,
  `bind.TestBind_CountsWorkLinearInTheDocument`,
  `bind.TestBind_CountsWorkLinearAcrossHeldDocuments`, `bind.TestBind_ReadsEveryWebhookAsAPathItem`,
  `e2e.TestReferenceObjects_AnEndingSlashNamesAnEmptyMember`,
  `e2e.TestReferenceObjects_AnEndInTheSourceIsReadThereUnderSelf`,
  `e2e.TestReferenceObjects_AnEndInAnotherDocumentIsDeclaredThere`,
  `load.TestBindAll_BindsEveryRefInTheCorpus`, `bind.TestBind_ReadsWhatAnotherDocumentDeclares`,
  `bind.TestBind_ReadsBelowAnIDOfAnotherDocument`, `bind.TestBind_AnIDNamingItsDocumentReturnsToIt`,
  `bind.TestBind_AnIdentifierIsNoLocationToFetch`,
  `bind.TestBind_AHeldDocumentMayDeclareAnothersURI`,
  `bind.TestBind_ARefusedURIADocumentDeclaresIsAnIdentifier`,
  `bind.TestBind_ALocationADocumentDeclaresIsThatIdentifier`,
  `bind.TestBind_DeclarationOrderReachesNoAnswer`,
  `bind.TestBind_ADocumentNoPassReachesDeclaresNothing`,
  `bind.TestBind_TheSourceDeclaresOnceWhateverThePasses`,
  `bind.TestBind_AReadingNoPassSettlesCountsEveryDocument`,
  `e2e.TestReferenceObjects_ARepeatedKeyIsReadAtItsLastCopy`,
  `bind.TestTable_SaysWhyARefWentUnread`, `bind.TestBind_SaysAKeyMayLiePastTheMergeBound`,
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
  `load.TestIndexWalk_ReportsAMediaTypeUseTheParserFaultsOnOnce`,
  `load.TestResolve_EveryKindOfObjectIsValidated`,
  `e2e.TestCompile_AMediaTypeReferenceLowersAlikeUnderAnUpperCasedScheme`,
  `operation.TestMediaTypeRef_ACarrierReadsTheDeclarationInItsDocument`,
  `e2e.TestReferenceIdentity_ASchemaRefThroughAnAliasIsOneNode`,
  `e2e.TestReferenceObjects_APointerThroughAPathItemRefReadsWhereItsChainEnds`,
  `e2e.TestReferenceObjects_AMergeListIsReadAsTheDecoderMergesIt`,
  `e2e.TestReferenceObjects_APointerMayHoldACharacterAboveUFFFF`,
  `e2e.TestReferenceObjects_AFailingChainIsReportedWhereItFails`,
  `e2e.TestReferenceObjects_AReferenceLandsOnAnObjectOfItsKind`,
  `e2e.TestReferenceObjects_AMergedRefIsAReference`,
  `e2e.TestReferenceObjects_NothingBesideARefIsRead`,
  `e2e.TestReferenceObjects_AReferenceInsideRawYAMLResolves`,
  `e2e.TestReferenceObjects_AnObjectThatCannotBeBuiltIsReported`,
  `e2e.TestReferenceObjects_AChainCrossesDocuments`,
  `load.TestBindingDiags_SitesEachFailureAtItsReference`,
  `load.TestBindingDiags_AChainIsReportedWhereItFails`,
  `e2e.TestCompile_ANullRefOfAReferenceObjectLeavesTheEntryOut`,
  `e2e.TestCompile_APathItemRefThatIsNoStringIsTheItemsOwnField`,
  `e2e.TestCompile_AnAdditionalOperationThatIsNoMappingIsLeftOut`,
  `e2e.TestCompile_AnAliasedNullRefIsReadWithoutAFault`, `load.TestAside_RestoreLeavesTheTreeAsWritten`,
  `load.TestLoad_TheTreeIsAsWrittenOnceTheModelIsBuilt`,
  `load.TestLoad_ANullRefOfAReferenceObjectIsReadWithoutAFaultAtEachModelBuild`,
  `e2e.TestCompile_AReachedObjectTheModelFaultsOnLowersAsUnderPaths`,
  `e2e.TestCompile_ARefAMergeKeyWritesThatIsNoStringLeavesTheEntryOut`,
  `e2e.TestCompile_AComponentWhoseRefIsNoStringIsKeptAsWritten`,
  `e2e.TestCompile_ReportsAReferenceNamingNothingBeneathANestedEncoding`,
  `e2e.TestGolden_EncodingNestedReferences`, `bind.TestPlace_ReadsBeneathAFieldNoModelHoldsAsRaw`,
  `e2e.TestCompile_AFieldNoModelHoldsReadsAsAnExtension`,
  `e2e.TestCompile_ReadsThroughAReferenceIntoANestedEncodingKeptAsRawYAML`,
  `e2e.TestCompile_RefusesAReferenceToNoSchemaBeneathANestedEncoding`,
  `statement.TestRead_PastTheMergeBoundNoKeywordIsHalfRead`,
  `statement.TestRead_PastTheMergeBoundTheSchemasOwnKeysStay`,
  `statement.TestRead_PastTheMergeBoundEachReaderReadsNeither`,
  `statement.TestRead_PastTheMergeBoundEachSchemaIsReportedOnce`,
  `statement.TestRead_AKeywordMergedPastTheBoundHasNoKeyAndNoValue`,
  `statement.TestFill_ATruncatedSchemaHoldsNoKeywordItsKeysDoNotList`,
  `statement.TestRead_PastTheMergeBoundAnOutrankedKeyIsLeftOut`,
  `statement.TestRead_PastTheMergeBoundAKeyNoParserNodeTellsStaysOnlyWhereWritten`,
  `statement.TestFillKeywords_EachComparesTheNodeTheParserReadItFrom`,
  `statement.TestRead_PastTheMergeBoundABodyIsReadAsItsTypeIs`,
  `schema.TestRef_ASchemaMergingPastTheBoundIsReportedOnce`
- Status: partly built: a redirected document is based at the URL requested (#1943). Through a proxy, the address the proxy gives a name is judged by nobody (#1947). No deadline bounds a compile's reads together (#1036). A name resolving to several address spaces is answered by the order of asking (#1969). The walk of a form body's parts reads what the parser expands past the merge bound, where every other reader reads the keys (#2038).

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
rest of the document lowers. A loop of Reference Objects is found and reported the same way, its
kind named in the report (`never reaches a request body`), and neither its members nor what leads
into it lower or draw a report of their own. A pointer whose read passes a reference object whose
own read needs the first, as `A: {$ref: '#/components/responses/B/headers/H'}` beside
`B: {$ref: '#/components/responses/A/headers/H'}`, is no loop but a read no order can make: each
such `$ref` is an `openapi/unresolved-ref` error ([`references`](#references)). A schema reaching
itself through what it declares is recursion, not a cycle ([`recursion`](#recursion)). An alias scan
that aborts on a fault of its own is reported as an `openapi/cycle-scan-failed` warning, and the
compile proceeds.

- Codes: `openapi/cyclic-ref`, `openapi/cycle-scan-failed`
- Tests: `e2e.TestCompile_SchemaRefCycleIsReportedAndStillCompiles`,
  `e2e.TestCompile_CycleIsReportedAtItsLeastMemberInEitherOrder`,
  `e2e.TestCompile_ReentrantPrefixReportsAlikeInBothDeclarationOrders`,
  `e2e.TestCompile_AliasCycleIsRefused`, `load.TestLoad_ADegenerateCycleLoadsAndIsReportedOnce`,
  `e2e.TestCompile_ALoopThroughAnotherDocumentIsReportedOnce`,
  `e2e.TestCompile_ABranchOnALoopIsReportedOnlyAsTheLoop`,
  `load.TestBindingDiags_NamesALoopsMembersByTheirDocument`,
  `bind.TestBind_ReportsALoopOutsideTheSourceAtItsLeastMember`,
  `bind.TestBind_ReportsALoopOfDefinitionsAtItsLeastMember`,
  `e2e.TestCompile_MergeChainPastBoundStillCompiles`, `bind.TestBind_AnswersAlikeInEitherOrder`,
  `e2e.TestReferenceObjects_ALoopIsReportedOnceAtItsLeastMember`,
  `e2e.TestReferenceObjects_AReadNeedingItselfIsUnresolved`

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

A document's `$self`, resolved against where the document was read, is its base and a name of it,
as OpenAPI 3.2 has it, in the source and in every document a reference reads. Every relative
reference in it, a schema's, a mapping value or a reference object's, is read against it, as under
an `$id`: judged for refusal as written while only a relative `$self` moved the base, and as the URI
it resolves to once an absolute one did. A reference naming the document by its `$self` lands in
it with no read, a security requirement key among them, and a schema whose `$id` names it roots no
resource by that `$id`, so its `$schema` chooses a dialect only where its place makes it a root, as
a Schema Object no other holds is ([`dialect-keywords`](#dialect-keywords)). Another document's
`$self` is declared at its root as an `$id` is. The location the document was read from still
decides what it may reach: a `$self`
never moves a fetched document's reference onto a file, which is an `openapi/unresolved-ref`
error where the reference is written. A `$self` holding a `#` is the rule table's format error at
`#/$self`, and names nothing. So a 3.2 document whose `$self` is an http(s) URL
reads the documents its relative references name from that URL, not from the files beside it; a
`locate` option ([`references`](#references)) maps the URL to them.

- Cases: `self-uri`
- Codes: `openapi/later-version-construct`, `openapi/unresolved-ref`
- Tests: `openapi.TestCompile_RecordsSelfURI`,
  `e2e.TestReferenceObjects_ADocumentPartIsReadWhereTheSourceLies`,
  `e2e.TestCompile_ASelfURIIsTheNameOfTheDocumentAndItsBase`,
  `e2e.TestCompile_ARelativeReferenceUnderAnHTTPSelfNamesAURL`,
  `e2e.TestCompile_ASelfURIMovesNoFetchedDocumentOntoAFile`,
  `e2e.TestCompile_ARelativeSelfIsResolvedAgainstWhereTheDocumentWasRead`,
  `e2e.TestCompile_ASelfNamingTheIdentifierOfASchemaRootsNothing`,
  `e2e.TestCompile_ARequirementKeyNamingTheSelfNamesTheSchemeOfTheSource`,
  `e2e.TestCompile_ASelfHoldingAFragmentIsAnErrorAndNamesNothing`,
  `e2e.TestCompile_NothingIsMappedByInference`,
  `e2e.TestCompile_ASourceReadOverTheNetworkReadsNoLocalFile`

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
and a scalar under a tag of the document's own as its text, since that tag claims no kind its text
could contradict. A field a reader finds is found the same way, so one supplied through a merge key
or an alias is kept as a written one is. A key a mapping writes twice is held at the copy the model
reads, the mapping's own last and a merge source's first
([`duplicate-key`](../parser-library.md#duplicate-key)), so a kept value agrees with the typed IR
read beside it, and the rule table warns of the repeat once, at its later copy, as an
`openapi/validation` warning of the class `validation-duplicate-key`: in a value it reads as no
object, an extension's, an undefined key's or an `example`'s, as in an object
([`validation`](#validation)). A node no JSON can hold — `.nan` or `.inf`, a scalar whose text its
tag does not admit, which
[`values`](#values) refuses for the reason given there, invalid UTF-8, an alias that cycles or
expands past the converter's node budget, a merge chain deeper than the reader expands — reaches the
IR in no form, and so does a number past `ir.MaxDecimalExp`, which the reader a lowered value goes
through refuses, so the two never read one scalar as two kinds. It is reported as an
`openapi/unpreservable-construct` error at the construct, and the rule that would have kept it
announces nothing as kept, so no diagnostic claims what the Document does not hold. A vendor
extension that converts to no JSON is [`vendor-extensions`](#vendor-extensions)' instead.

- Cases: `yaml-merge-keys`
- Codes: `openapi/unpreservable-construct`
- Tests: `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`,
  `schema.TestCoDeclaredFamily_UnpreservableIsNotAnnounced`,
  `schema.TestConstraints_ACountPastTheDecimalRangeIsRefused`,
  `schema.TestAllOf_TheBindingCountPastTheDecimalRangeIsRefusedInEveryOrder`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`,
  `annotation.TestRawFromNode_PreservesMergeAndOrderingSemantics`,
  `annotation.TestRawFromNode_KeepsTheLastCopyOfARepeatedKey`,
  `annotation.TestScalarRenderers_AgreeOnKind`, `e2e.TestCompile_ARepeatedKeyIsKeptAsItsLastCopy`,
  `e2e.TestCompile_ARepeatedDefaultIsKeptAsItsLastCopy`,
  `e2e.TestCompile_ARepeatedKeyIsReadAsItsLastCopyInEverySpelling`,
  `statement.TestSchema_KeyReadsAKeywordAsTheParserDoes`

### `unmodeled-dialect-fields`

A source revision may add a field the bundled parser's model names no member for; OpenAPI 3.2's
`Response.summary`, XML `nodeType`, the nested Encoding fields, a Media Type Object's `description`,
`components/mediaTypes` and a content entry written as a Reference Object are the live set. Such a
field is read off the raw node by a reader of its own, and the object census is *told* which keys
that reader took: the census grades a key by the parser's model, so left alone it keeps a field the
document defines as an undefined key. The reader runs whatever version the document declares, as
[`later-version-construct`](#later-version-construct) requires, so the census never keeps such a
field as undefined.

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
([`unreferenced-components`](#unreferenced-components)). Nothing models the nested Encoding fields.
The binder places each field the rule table gives an object outside a schema that can hold a schema
or a Reference Object, at every minor and whatever the parser's model holds, so a `$ref` written
beneath a nested Encoding Object is bound and reported as any other is, and a schema one lands on
there is built from the raw YAML, as one an extension holds is.

- Cases: `response-summary-32`, `xml-nodetype-32`, `nested-encoding-32`, `component-media-types-32`
- Codes: `openapi/degraded-construct`, `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryNoReaderTakesIsStillReported`,
  `annotation.TestRead_NodeTypeOnlyADefinedValueIsPlaced`,
  `operation.TestContent_MediaTypeDescriptionReachesItsDocs`,
  `document.TestComponentMediaTypes_AnUnreferencedEntryIsKept`,
  `e2e.TestCompile_AComponentWhoseRefIsNoStringIsKeptAsWritten`

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
entry is kept under its own name, and an entry whose key is no scalar is left out
([`validation`](#validation) reports the key). An entry whose `$ref` names nothing, a null or a
list among them, which every reader takes for the entry left out ([`references`](#references)), is
kept as the source wrote it, beside the rule table's error at the `$ref`. A key an entry writes that
its object does not define is reported all the same, by the rule table
([`unknown-keys`](#unknown-keys)), and rides inside the kept entry. Where the components merge
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

`no_ir_home` rather than a boundary, and no diagnostic: the gap is one the IR can close by finding a
use for the entry, so a promotion pass should read it, and a document that declares a component is
not making a mistake a warning could name. `Reason` is where that decision lives
([unmodeled](../ir/unmodeled.md#reasons)), which is what keeps a promotion pass from mistaking one
for an oversight.

- Cases: `unreferenced-components`, `component-media-types-32`
- Codes: `openapi/degraded-construct`
- Tests: `document.TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget`,
  `document.TestUnreferencedComponents_ReadAsTheParserReadsThem`,
  `document.TestUnreferencedComponents_SelfReferenceByFileNameReaches`,
  `document.TestUnreferencedComponents_MergeDepthBoundIsReported`,
  `document.TestComponentMediaTypes_AnUnreferencedEntryIsKept`,
  `e2e.TestCompile_AComponentWhoseRefIsNoStringIsKeptAsWritten`

## 3. Names and values

### `naming`

A name the document spells lowers to `Naming{Source, Canonical}`: the spelling as written, beside
the word sequence [naming](../ir/naming.md) derives from it, at every position that spells one —
`info.title`, a tag, an `operationId`, a parameter, response-header or property name, a component
schema or security scheme key, a scheme a security requirement names, an enum member's value as
text, a 3.2 server `name`, and under `grouping=path-prefix` a first path segment. A name with no
word character (`***`, `>=`, or only variation selectors) keeps its `Source` and has no
`Canonical`; an emitter names it by its characters ([naming](../emitters/refine/naming.md)). A name
spelled as the empty string is minted `Hint: "empty"` and no `Source` rather than passed through (an
`operationId` of `""` is read as absent instead, [`operation-ids`](#operation-ids)), and a
property's wire name stays `""`. A component schema keyed `""` earns no named `TypeID`: it hoists
anonymously at its own pointer (`t/anon/components/schemas/`) with that hint. An entity the document
gives no name — an inline schema, a union variant, a response
([`response-conditions`](#response-conditions)), a server with no `name` (its URL's words, else
`server`), the synthesized `default` and `webhooks` groups — carries a `Hint` alone, in the same
word grammar and composed from its position, each half neutralized first so an empty parent gives
`empty_item`, never `_item`. A request body or header that references reach is named for its
declaration, never for whichever reference lowered first: a component's name, a header's own key,
the `operationId` of the operation declaring the body, in this document or another and whether or
not that operation is lowered, or `request` for a body no operation with an `operationId` declares.
A schema is named for the nearest object holding it, so one a parameter, request body or response
holds is named for that object even where the object is kept under an extension and read through a
`$ref`, and a document a `$ref` reads whole as a schema is named for the document (`./Cat.yaml`
names its root `cat_yaml`). A `components/mediaTypes` entry is named by its key, `""` minted as a
component schema's is, and a header of an Encoding Object that another nests through 3.2's
`encoding`, `prefixEncoding` or `itemEncoding` by its key, as any other header is. Every such name
is a function of the documents and the position alone, read where the position is written, so no
lowering order changes it: a union variant, or a composition
branch holding a `$ref`, is named as the position the `$ref` lands on, in whichever document, and
keeps its own name where that names nothing. An inline `allOf` branch is a conjunct of the schema
holding it, so a node built there ([`allof-refinement`](#allof-refinement)) is named as that schema
is. `Aliases` is never set.

A schema whose `title` holds a word character is named by the title's words instead of its
position, wherever the grammar reads a schema: a union branch, `by_name` for
`{title: By Name, …}` where its position gives `variant_0`, ahead of the name a `$ref` beside it
suggests; an inline schema under a property or a role; the schema of a parameter, header, request
body or response; and a document a `$ref` reads whole. The positions beneath it compose from that
name, and the title stays the node's `Docs.Summary`. A schema writing no title takes the one the
inline branches of its `allOf` write between them, each conjunct describing the same instance, where
they write one set of words, and none where they write two; a branch holding a `$ref` lends no
title, and one nested in a branch is not read. A title is read whole, one that is a sentence giving
all its words, and as the parser reads a string, so one under `!!binary` or a local tag names
nothing. It names nothing where the document names the schema by a key: a component schema, and a
`$defs` or `definitions` entry, which keep their keys. It names nothing at an `allOf` branch, named
as its holder, and nothing where its words are a component schema's canonical name in any document
read, or another branch's of the same `oneOf` or `anyOf`, written as its title or as its position
(`variant_2`): the position keeps the name it has without the title, so two branches titled alike
are both named by position, whichever is written first. A title is compared with no name a sibling
`$ref` takes from the position it lands on, which the component names cover where it lands on a
component, so a pair meeting elsewhere is the names pass's to report (`pass/name-collision`), since
comparing would resolve every sibling's chain for each branch. A hint composed from a position is
mechanical, while one read from an annotation is a reading, so policy (invariant 6): JSON Schema
declares `title` a label for display, a request body titled with a sentence under
`operationId: createThing` is named by its dozen words where its position gives
`create_thing_request`, and editing a title renames the node. The title-names policy, on by
default, is the Go option `Options.TitleNames`, whose `Disabled` names every anonymous schema by its
position, and the text option `title-names`, which `false` turns off; the title is the node's
`Docs.Summary` either way. A hint read under it is marked no `Inferred`, since a `Hint` with no
`Source` is a suggestion already. A hint changes no ID. A const branch's title is its member's
summary alone ([`string-const-union`](#string-const-union)).

These names are what an emitter renders, so a change to how one is derived moves `IRVersion`
([ir-version](../ir/ir-version.md)), and `testdata/golden/openapi/hint_positions.yaml` writes the
positions within one document that a hint is composed from, one for each way the naming composes
one.

- Cases: `neutral-naming`, `empty-names`, `title-hints`
- Tests: `ids.TestComponentSchema_ClassifiesEveryPointer`,
  `schema.TestEnumMemberIDs_NamesThatCanonicalizeToNothingStillGetIDs`,
  `schema.TestInlinePosition_HintIsTheSameInBothOrders`,
  `operation.TestContent_ASharedBodyOrHeaderIsNamedForItsDeclaration`,
  `schema.TestComponentBody_SubtreeIsNamedByItsDeclaration`,
  `schema.TestUnionVariant_UnderPathsIsNamedAsTheNodeItHolds`,
  `schema.TestUnionVariant_AWholeDocumentIsNamedForIt`, `naming.TestAt_NamesASchemaAfterItsOwner`,
  `naming.TestAt_NamesABranchAfterItsTarget`, `naming.TestAt_NamesAStructureAReferenceReadsInsideRawYAML`,
  `naming.TestAt_NamesASchemaByItsTitle`, `naming.TestAt_NamesNoSchemaByItsTitleWhereTitlesNameNothing`,
  `lowering.TestTitleNames_TheZeroValueNamesByTitle`, `e2e.TestTitleNames_DisabledNamesByPosition`,
  `naming.TestAt_ATitleNamesAlikeInEitherOrder`,
  `naming.TestAt_ATitleNamesAcrossDocuments`, `naming.TestTitle_IsTheNameATitleGives`,
  `naming.TestAt_NamesAHeaderAnEncodingNests`, `schema.TestRefinement_TwinsTheRefSpelling`,
  `e2e.TestGolden_HintPositions`, `e2e.TestCompile_NamesAHeaderSchemaBeneathEveryEncodingByTheHeader`

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
scalar that YAML resolves to a string only because it is past float64's range is still the number
it spells, never its text: a decimal literal a `BigVal` holds (its exponent within
`ir.MaxDecimalExp`) is that number, and one past that exponent is a number no `Value` holds, as the
same literal tagged `!!float` is. A quoted one stays a string. A value no `Value` holds — `.inf` or
`.nan`, a number past `ir.MaxDecimalExp`, a `!!binary` that is not base64, a scalar whose text its
tag does not admit (`!!null abc`, `!!timestamp soon`), a tag the compiler does not lower (`!acme`), a
key a mapping or a mapping merged into it writes twice, a merge chain deeper than the reader
expands, nesting past the reader's depth bound — is not coerced: an example is dropped with an
`openapi/degraded-construct` warning at its position, a default with an `openapi/unlowered-construct`
warning at its own `default`, in whichever document and once however many carriers read it, each of
which then takes none, and a `const` or an enum member is [`enum-const`](#enum-const)'s. A scalar whose text its tag does not admit is refused rather than
read as its text, since its tag says what kind of value it is, YAML's own decoding refuses it, and
its text read as a string would be a value of a kind the source never wrote, where the warning says
what the document got wrong. The number reader is the numeric bounds' as well
([`constraints`](#constraints)). A value kept verbatim instead is
[`verbatim-preservation`](#verbatim-preservation)'s.

- Cases: `defaults`, `yaml-timestamp-scalars`, `numeric-precision`, `yaml-merge-keys`
- Codes: `openapi/degraded-construct`, `openapi/unlowered-construct`
- Tests: `value.TestNumericLiteral_YAMLBases`, `value.TestValueFromNode_Timestamp`,
  `value.TestValueFromNode_Scalars`, `value.TestValueFromNode_ReadsAMappingAsTheParserDoes`,
  `value.TestValueFromNode_RefusesWhatNoObjectHolds`, `value.TestClassify_ReadsEachSpellingOnce`,
  `value.TestClassify_RefusesAPlainNumberPastTheExponentRange`,
  `annotation.TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped`,
  `schema.TestUseDefault_ReportsAtTheDefault`, `schema.TestUseDefault_ReportsAHeadersDefaultAtTheDefault`,
  `schema.TestUseDefault_AChainReportsAtTheHopThatSupplied`,
  `operation.TestParams_AnUnconvertibleDefaultIsReportedWhereItIsWritten`

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
its position or read from its title ([`naming`](#naming)), and the position holds a `TypeRef` to
it. One whose lowering is a primitive, `{type: string}` with or without bounds beside it, hoists
nothing: the position names the shared primitive, and what it declares beside the type lands on the
property or parameter that carries it ([`constraints`](#constraints)).

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
([`later-version-construct`](#later-version-construct)), and the annotations each hop of its `$ref`
chain declares, nearest first — documentation, deprecation, redaction, visibility and `default` — merge
onto the property,
parameter or header that references the target, field by field, a keyword the use site writes
winning over the target's ([annotations](../ir/annotations.md#what-a-reference-carries-down)), and by
the same rule at every carrier. Redaction has nothing to win with: `format: password` on either side
asks for it, and no keyword beside the `$ref` withdraws the target's. Nor do `deprecated`,
`readOnly` and `writeOnly`: JSON Schema reads each in force at a location where any occurrence
applying to it is true, and an occurrence `false` as one omitted, so `deprecated: false` beside a
`$ref` to a deprecated target leaves the use deprecated, in the 3.1 spelling and in the `allOf`
decorator spelling alike, whose [fold](../emitters/refine/decorators.md) reads the use deprecated
where any of its annotations is. An extension the promotion
policy maps is read the same way ([`extension-promotion`](#extension-promotion)). At a position that
carries no property, parameter or header — an `allOf`, `oneOf` or `anyOf` branch, an `items`, a
component — the keywords beside the `$ref` bind an alias hoisted at that position instead
([models](../ir/models.md#composition-entries)). Constraints are the exception: bounds conjoin rather
than override, so each position keeps the ones it declared and none is copied to a use site
([annotations](../ir/annotations.md#what-a-reference-carries-down)).

A refinement ([`allof-refinement`](#allof-refinement)) is a use of what it names, so a carrier holding
one reads its target by this rule, and one holding a `$ref` to a refinement reads through it, up to 64
refinements, an alias between two spending none of them: what the target declares, what its inline branches lend it
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
  `schema-ref-siblings-30`, `allof-refinement`, `ref-chain-annotations`
- Tests: `e2e.TestAnnotationRetention`, `schema.TestModel_RefSiblingDescriptionWins`,
  `schema.TestSchemaExamples_ComponentRefSiblingKeepsThem`,
  `schema.TestRefinement_ACarrierReadsThroughARefinement`,
  `schema.TestUse_ADefaultBesideItsValueSetStands`,
  `schema.TestUse_ADefaultNoNarrowingExcludesReadsAsBesideARef`,
  `schema.TestUse_ACarrierReadsThroughADecorator`,
  `schema.TestUse_ALoopOfNarrowingEnumsEnds`, `annotation.TestEffectiveDeprecated_AnySchemaDeprecates`,
  `annotation.TestEffectiveVisibility_MapsTheFlagsToLifecycles`,
  `operation.TestParams_RefSchemaUseSiteWinsOverItsReferent`, `schema.TestUseOf_LendsEachHopOfTheChain`,
  `schema.TestUseOf_LendsEachHopAcrossTwoDocuments`, `schema.TestUseOf_AnAliasHopIsNoPassage`,
  `schema.TestUseOf_PassagesAreBoundedAndAliasesSpendNone`,
  `openapicases.TestCore_SpellingsHoldTheRecord`

### `reference-object-siblings`

A Reference Object's own siblings are the annotations of the use site. OpenAPI lets the entry that
writes a `$ref` carry `summary` and `description` beside it, and the pair describes *that use* of
the referenced object rather than the object. Where the IR has a docs field at the position — a
parameter, a response or error case, a header, an example, a request body, a security scheme, a
body's media type, a link — both are read off the entry and folded over the declaration's docs field
by field, a sibling the entry writes winning and one it omits leaving the declaration's value
standing.
A component that is itself
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

Every other key a Reference Object of the chain writes beside its `$ref`, read as the model reads the
mapping, a `<<` included, rides on the node the use lowers to, under
`openapi:reference/<i>/<key>`: `i` is 0 for the use, then counts the aliases the chain passes,
nearest first, so a use and an alias writing one key are two entries. Each is kept `out_of_scope`,
OpenAPI giving it no effect, with its own key's provenance in the document writing it; the rule
table reports it ([`unknown-keys`](#unknown-keys)), and keeping it reports nothing more. Where the
position has no docs field, a callback's and a parameter's or header's content entry's, the use folds
nothing, so the `summary` and `description` each Reference Object writes are kept the same way,
`no_ir_home`, each reported as an info `openapi/unhomed-construct` at its key, and the entry leaves
when a docs field arrives. The Media Type Object's own keys keep `openapi:content/<mt>/<key>`, which
a key of the chain's never shares: a key is read whole, never by its prefix.

| Use | Node the entries ride on | Scope before `reference/<i>/` | Folds docs |
|---|---|---|---|
| parameter, header | the `ir.Parameter`, the header's `ir.Property` | none | yes |
| request body, response, example | the operation's `Request`, the `ir.Response` or `ir.ErrorCase`, the `ir.Example` | none | yes |
| link | the response's link | none | yes |
| callback | the operation's HTTP binding | `callbacks/<name>/` | no |
| media type of a body | the `ir.Content` | none | yes |
| media type of a parameter's or header's content | the parameter or the header's property | `content/<mt>/` | no |
| security scheme | the `ir.AuthScheme` | none | yes |

A path item's fields beside its `$ref` are its own ([`path-item-ref-fields`](#path-item-ref-fields)).
Rejected: keeping the keys on the Document by the use's coordinate, which nothing on the node leads
to; and one flat `reference/` scope, which would make a use and an alias writing one key one entry.

- Cases: `ref-site-docs`, `later-version-constructs`, `component-media-types-32`, `ref-beside-keys`
- Codes: `openapi/later-version-construct`, `openapi/unhomed-construct`
- Tests: `operation.TestMediaTypeRef_UseSiteDocsFoldAlongTheChain`,
  `operation.TestMediaTypeRef_DocsAnotherDocumentsAliasWritesArePlacedThere`,
  `operation.TestContent_MediaTypeRefIsFollowed`,
  `operation.TestMediaTypeRef_ABodyFoldsAndAParameterKeepsEachPosition`,
  `operation.TestLinks_UseSiteDocsFoldOverTheDeclaration`, `service.TestBeside_KeysTheUseAndEachAlias`,
  `service.TestBeside_ABareReferenceCostsNothing`, `operation.TestContentDocs_ChainAndDeclarationKeepApart`,
  `operation.TestContentDocs_AKeyIsReadWhole`, `operation.TestCallbacks_AUseKeepsItsDocs`,
  `e2e.TestBeside_AnAliasInAnotherDocumentKeepsItsKeyThere`,
  `e2e.TestBeside_ASecuritySchemeKeepsTheKeysBesideItsRef`,
  `operation.TestMediaTypeRef_ABodyKeepsTheKeysBesideEachRef`,
  `operation.TestMediaTypeRef_ADocKeyNotKeptDrawsNoKeptInfo`

### `use-bound-annotations`

`readOnly` and `writeOnly` lower to the `Visibility` of the property or header that uses the schema
— `readOnly` to `Only: [read, delete, query]`, `writeOnly` to `Only: [create, update]`
([properties](../ir/properties.md#visibility)) — and a schema's `default` to the `Default` of
the property, parameter or header that uses it, never to the type: both bind a *use* of the type,
not the type. One a declaration writes, on a component or any other position that owns a node of its
own, is therefore pushed down to each property, parameter or header referencing it that has a field
for it, as [`ref-use-site`](#ref-use-site) merges them: a `default` with use-site precedence, and
`readOnly` and `writeOnly` in force where either side sets them, and the declaration keeps its
own copy verbatim under `no_ir_home`, reported with an info `openapi/degraded-construct` at the
keyword. That copy is what a component nothing references would otherwise lose in silence. A
`default` no `ir.Value` holds is pushed down to no carrier, as [`values`](#values) reports where it
is written, and its info says so. A
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
keyword states no message, version, date or replacement, which only
[`extension-promotion`](#extension-promotion) can supply, and which can state the deprecation itself
where no keyword does; a replacement the message it supplies names is read too
([`replacement-inference`](#replacement-inference)). An operation, and a
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

### `replacement-inference`

The replacement policy, `Options.ReplacementInference` and the text option `replacement-inference`
([`policies`](#policies)), reads what a deprecation's message names as its replacement into
`Deprecation.ReplacedBy` ([replacements](../ir/annotations.md#replacements)). It reads the message
[`extension-promotion`](#extension-promotion) fills on three classes of deprecated node, each where
no key the promotion maps to `deprecation.replacedBy` is written beside it, and reports a message
naming a replacement it cannot settle as an info `openapi/undecided-replacement`:

1. An operation's message names an operation by its route: a word that is a method an operation of
   the document is bound with, a backtick or a parenthesis before it aside, then, after white
   space, a path starting `/`, in a code span or out of one, up to white space or a backtick. So
   `` Use `POST /v2/zones` instead. ``, ``the GET `/v2/zones` endpoint`` and
   `Use GET /v2/zones before 2026-11-28.` each name a route. A route names the operation whose
   binding has that method and that path template as written, or, naming none, with the
   punctuation a sentence puts after a path (`.`, `,`, `;`, `:`, `!`, `?`, `)`) cut from its end.
2. A parameter's message names, in each code span, a parameter of its operation, as a replacement
   key names one: `<in>.<name>`, or a name one parameter carries.
3. A model member's message names, in each code span, a member of its model by name, a member the
   model's composition flattens in among them, as a replacement key names one.

A code span is read as CommonMark reads one. A name is read only where a phrase introduces it as a
replacement, within one sentence, its words compared in lower case, in one forward pass over the
message:

| Part | Rule |
|---|---|
| cue | `use`, with nothing before it in its sentence but words ending in `please`, `should`, `must` or `instead` (a comma after one aside); or `renamed to`, `replaced by`, `replaced with`, `superseded by`, `in favor of`, `in favour of`, anywhere in the sentence |
| negation | none of `not`, `never`, `no`, `cannot`, or a word ending `n't`, before the cue in its sentence |
| filler | then at most `the`, `a`, `an`, `new`, and an opening `(` or quote; a route's method word outside its code span belongs to the name |
| name | a route for an operation, a code span for a parameter or a member |
| more names | each joined to the one before by `,`, `and` or `or` alone |
| sentence end | `.`, `!`, `?` or `;` followed by white space or the end, or a blank line, none inside a name |

So ``Use `a`, `b` or `c`.`` names three, ``Do not use `b`; use `a`.`` and ``Use `a` instead of `b`.``
name `a` alone, and ``Unlike `GET /v0/zones`, this pages.``, ``Formerly `b`.``,
``If you use `c`, set `d`.``, ``You may use `c`.``, ``Deprecated: use `c`.`` and
``This was not replaced by `c`.`` name none. A name no phrase introduces is neither read nor
reported. The policy's trigger is a message introducing an entity of the node's class other than
the node: one introducing none, or the node alone, fills nothing and is not reported, since prose
naming something else is no defect. Where it introduces one, the names fill the field all or none,
the node itself passed over and a repeat kept once: a name naming no entity, or several, leaves the
field unset, reported at the node, quoting the first such name. The message keeps its prose either
way. The reading runs once the document is whole, as a replacement may be declared after what it
replaces. A Markdown link's title is display text and its URL a page of documentation, neither
naming an entity the document holds, so a link is never read; nor is a header's, a schema's or a
security scheme's message, where no measured document names a replacement such a scope resolves.
On the golden spec
(spec @86556f6f), of the 217 operations whose deprecation carries a message, 25 name a route the
document declares, 16 in a code span and 9 beside one, and take it; 10 name a path relative to the
prefix the document mounts it under, which names no route as written, and 146 a link, and take
nothing; 2 parameters and 32 members take the sibling a code span names. Each of the 59 is the
replacement its message tells a caller to use, and no message is reported.

- Cases: `replacement-inference`
- Codes: `openapi/undecided-replacement`
- Tests: `replacement.TestMessage_AnOperationsNamesItsReplacementByRoute`,
  `replacement.TestMessage_ARouteTwoOperationsShareNamesNone`,
  `replacement.TestMessage_AMembersAndAParametersNameSiblingsInCode`,
  `replacement.TestMessage_ANameTwoParametersCarryIsReported`,
  `replacement.TestMessage_YieldsToWhatTheDocumentSays`,
  `replacement.TestCodeSpans_ReadsCommonMarksSpans`,
  `replacement.TestMessage_ReadsOnlyIntroducedNames`, `replacement.TestMessage_ReadsEachCueAndNoNearMiss`,
  `replacement.TestMessage_CostsItsLength`, `replacement.TestMessage_UseOpensItsSentence`,
  `e2e.TestConventions_DeclarationOrderChangesNothing`, `e2e.TestConventions_DisabledFillsNothing`
- Status: open: a `use` after a colon, a parenthesis or a dash is no cue, so
  ``Deprecated: use `b` instead.``, which `main` read, names nothing (#1956).

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
nullable reference that `{$ref: T, nullable: true}` was meant to be. So the nullable-inference
policy, on by default, reads such a null as widening what it is written on rather than conjoining
with it: `nullable: true` admits null for the whole schema it is written on, whatever else that
schema says, and keywords beside a `$ref` that admit null by themselves admit it for the whole
reference, and a `nullable: true` on an inline `allOf` branch writing no `properties` admits it for
the whole schema holding the `allOf`, a `type` beside the `allOf` included, as beside the `allOf` it
does ([`allof-inline-residue`](#allof-inline-residue)). A `type` without `null` beside a `$ref` still
narrows what it lands on, and a `type: [T, "null"]` on a conjunct of an `allOf` conjoins with the
rest. A reference whose null only that reading admits names `nullable-inference` in
`TypeRef.Inferred` (invariant 6), so `Nullable` with no marker is the declared reading, and each
such null is reported once as an info `openapi/inferred-nullable`, at the `nullable` keyword or, for
keywords beside a `$ref`, at the schema that writes them, saying why its version does not admit it,
whatever references its schema. The policy is the Go option `Options.NullableInference`, whose
`Disabled` reads the keywords only as their version defines them, and the text option
`nullable-inference`, which `false` turns off. A `nullable` in a 3.1 or 3.2 schema that the policy
does not read is a keyword its dialect does not define, kept verbatim under `openapi:nullable`
(`out_of_scope`) with an info `openapi/unknown-schema-keyword`, where [`unknown-keys`](#unknown-keys)
keeps a keyword the schema model does not name: every one with the policy off, and with it on every
one that is not the boolean `true`, a `false` among them, wherever it is written, beside a `$ref`
included. Written on an inline `allOf` branch or a union's null branch, it keeps the branch whole
as the policy off does, since no fold reads it. Under either policy only the boolean `true` states a
null: a `nullable` holding a string states none at any version, a YAML 1.1 truth word the parser
decodes as true, such as `"yes"` or a plain `on`, among them, and neither does any other value that
is no boolean. A `null` written for `nullable` is the keyword left out
([`null-keywords`](#null-keywords)).

Whether a 3.0 `nullable: true` has an effect is a fact of the specification, not of the policy: with
no `type` written in the same Schema Object, a `type` written `null` being none, it has none, beside
an `allOf`, `oneOf`, `anyOf` or `enum` too, and is reported as an `openapi/validation` warning of the
class `validation-no-effect`, at the keyword ([`validation`](#validation)), under either policy.
With the policy on, its info sits at the same pointer. With the policy off nothing keeps it:
`TypeRef.Nullable` holds its declared reading, `false`, and the warning is its record. Beside a
`$ref` the keyword is a later minor's construct, warned of as one
([`later-version-construct`](#later-version-construct)), so it draws no second warning there.

- Cases: `nullable-30`, `nullable-31-ref`, `nullable-enum-31`, `nullability-four-states`,
  `nullability-conjunction`, `nullable-keyword-30`, `nullable-keyword-31`
- Codes: `openapi/inferred-nullable`, `openapi/unknown-schema-keyword`, `openapi/validation`
- Tests: `schema.TestSchemaRef_NullableNormalization`,
  `schema.TestNullableInference_EachShapeReadsByItsVersion`,
  `e2e.TestNullableInference_AKeywordIsReportedOnceAtItself`,
  `e2e.TestNullableInference_DisabledKeepsWhatTheDialectDoesNotDefine`,
  `e2e.TestSchemaRefSiblings_ReadInTheDeclaredDialect`,
  `e2e.TestNullableInference_ACompositionKeepsTheMarker`,
  `merge.TestIntersectNull_TheMarkerFollowsTheBit`,
  `schema.TestNullableInference_ABranchWidensTheAllOfHoldingIt`,
  `oas.TestCheck_NullableAppliesBesideAType`,
  `e2e.TestNullableInference_ANullableWithNoTypeIsWarnedOfUnderEitherPolicy`,
  `schema.TestNullableInference_OnlyABooleanStatesNull`,
  `schema.TestNullableTrue_PastTheMergeBoundOnlyAListedKeyStates`,
  `e2e.TestNullable_AValueThePolicyDoesNotReadIsKeptAtOpenAPI31`,
  `e2e.TestNullable_AValueNoPolicyReadsIsKeptOnANullBranch`,
  `e2e.TestNullable_OpenAPI30KeepsNoValueAsAnUnknownKeyword`

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

An `allOf` whose kinds are an object alone lowers to the composing `Model`'s `Base` and `Mixins`
([models](../ir/models.md#composition)); any other is [`allof-refinement`](#allof-refinement)'s. A
`$ref` entry composes only where the schema it lands on lowers to an object alone, null aside, or to
the union of a `type` list holding an object, whose object variant it composes (below): the sole
such entry becomes `Base`, and of several the first, in source order, to a schema taking part
in a discriminator hierarchy: one declaring a `discriminator`, or composing through `allOf`, at any
depth, a schema that does, the ancestors [`discriminator-value`](#discriminator-value) reads a tag
off. Every other such `$ref` entry becomes a `Mixin`, so a schema composing a subtype of a
discriminated base beside another `$ref` is a subtype of that base too. A `$ref` entry whose target
admits every kind, or another kind than an object, composes nothing, so no `Model` names a base or a
mixin that is no object: it is kept verbatim under `openapi:allOf/<n>` (`degraded_lowering`) with an
info `openapi/degraded-construct` at the entry, and its target still lowers. A composition meets one
only where its read is cut short or its conjuncts share no value, as `{allOf: [{$ref: List}], type:
object}` over an array does. The election reads the source, so no declaration order decides it, and a field
redeclared as several models composes them as such an `allOf` does
([`allof-redeclared-property`](#allof-redeclared-property)), reading the hierarchy off the
discriminator a model or a union holds and each model's supertypes. An inline entry merges into the
composing model's own `Properties`, each property keeping its own provenance
([`allof-inline-residue`](#allof-inline-residue),
[`allof-redeclared-property`](#allof-redeclared-property)). So every `$ref` entry stays composed
and a declaration a fold drops stays kept, while what a declaration folded into a field adds lands
on the field without its source. A `$ref` entry naming a union of objects is no exception: the union
is the `Base` or a `Mixin`, never distributed over its variants as one written beside the `allOf` is
([models](../ir/models.md#a-union-a-composition-names)). One naming a schema whose `type` list holds
an object and another kind composes that list's object variant, the one node of that kind its union
holds, standing at `…/type/<n>`: `Cat: {allOf: [{$ref: Pet}, …]}` over `Pet: {type: [object, string],
properties: …}` has the variant as its `Base`, so it inherits what `Pet` declares of an object and
keeps the tag `Pet`'s discriminator gives it. A union a `oneOf` or `anyOf` writes is not unpacked: it
may hold several objects, or none a model is, so one admitting another kind is kept as above. The
list is read only where the entry's `$ref` lands: one landing on another `$ref` to `Pet`, as
`{$ref: Chain}` over `Chain: {$ref: Pet}` does, is kept as above too.

`allOf` is associative, so an inline entry writing a non-empty `allOf` is flattened into the
enclosing list before the entries are classified: its own entries take its place, in source order
and depth first, each keeping the pointer it was written at, and its `properties` and `required`
merge after them, as a composing schema's own merge after its entries'. So `allOf: [{allOf: [A,
B]}]` classifies as `allOf: [A, B]`, and nothing is reported. Whatever else the entry writes that
the merge does not consume is kept beside the composition as residue
([`allof-inline-residue`](#allof-inline-residue)), so `{unevaluatedProperties: false, allOf: [A,
B]}` composes `A` and `B` and is also kept whole. Flattening stops 32 levels down: an entry nested inside
32 flattened entries is not flattened itself, so its `allOf` is residue, and it is reported as nested
past the flattening depth of 32. This is not the flattening the
[design rules](../ir/README.md#design-rules) leave to emitters: every `$ref` entry still composes as
a `Base` or `Mixin`, and all that goes is a grouping no instance can observe, which an inline entry
owns no node to keep ([`allof-inline-residue`](#allof-inline-residue)).

- Cases: `allof-inheritance`, `allof-mixins`, `allof-inline-merge`, `allof-nested-inline`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestAllOf_DiscriminatorHierarchy`, `schema.TestAllOf_SoleRefBecomesBase`,
  `e2e.TestAllOfCompose_ObjectsAloneStayAModel`, `e2e.TestAllOfCompose_ABaseThatIsNoModelIsKeptNotComposed`,
  `e2e.TestAllOfCompose_ConjunctsNoInstanceSatisfiesNameNoPlainBase`,
  `e2e.TestAllOfCompose_ASubtypeOfATypeListComposesItsObjectVariant`,
  `e2e.TestAllOfCompose_AMemberTheVariantAndTheSubtypeDeclareFolds`,
  `e2e.TestAllOfCompose_OnlyATypeListsObjectVariantIsComposed`,
  `schema.TestLower_ATypeArrayKeepsItsDiscriminatorOnTheUnion`,
  `schema.TestAllOf_NestedInlineWrapperFlattens`, `schema.TestAllOf_NestedWrapperBaseSelection`,
  `schema.TestAllOfBranches_DepthCapKeepsWrapperVerbatim`,
  `schema.TestAllOfBranches_DepthCapNamesWhatElseTheWrapperWrites`,
  `schema.TestAllOf_NestedWrapperWithResidueKeptVerbatim`, `schema.TestAllOf_NestedLowersAsFlat`,
  `schema.TestAllOf_AUnionItNamesComposesAsWritten`, `schema.TestAllOf_ARefToAHierarchyMemberIsTheBase`,
  `merge.TestFold_RedeclaredModelsCompose`, `e2e.TestCompile_SharedCompositionAncestorsDoNotAmplify`,
  `openapicases.TestCore_SpellingsHoldTheRecord`
- Status: partly built: a field redeclared as a hierarchy member that lowers to a `Union` holding no
  discriminator takes it as a `Mixin` where a written `allOf` takes it as its `Base` (#1901).

### `allof-refinement`

An `allOf` is a composition where the kinds the position lowers to, null aside, are an object
alone, the keywords beside the `allOf` being a conjunct as much as a branch is, and where its marker
is the kind-inference policy's where that reading is inferred
([`shape-applicator`](#shape-applicator)). Those kinds are what the conjuncts declare together, or,
where nothing declares one, what their shape keywords claim, so `{allOf: [{$ref: E}], description:
d}` over `E: {items: …}` is a use of the list the policy reads `E` as. A `Model` anywhere else
asserts an object the source rules out: `{allOf: [{type: integer}, {type: integer, format: int32}]}`
would reach a consumer as an object with no fields, where it means `int32`, and `{allOf: [{$ref:
AnyT}], minLength: 3}` over `AnyT: {}` as one excluding the strings it admits. Any other `allOf` is
a *refinement*: one use of the conjunct that decides it, its *principal*, with what the other
conjuncts say read as said of that use. So `{allOf: [{$ref: T}, {type: string}]}` refines a `T`
that may be a string or an object, as the two together admit no object, `{allOf: [{$ref: AnyT}],
minLength: 3}` is a use of `AnyT` holding the bound, as `{$ref: AnyT, minLength: 3}` is, and
`{allOf: [{$ref: Str}], properties: …}` is a use of `Str`, the `properties` only a `Model` holds kept
as below. Entries whose keywords speak of an object and an array, with no kind declared, admit both:
`{allOf: [{properties: …}, {items: …}]}` is the top type keeping each entry.

What the conjuncts admit is read from the source and the bindings, never the registry, so no
lowering order decides it. Each conjunct's JSON kinds and value set are read across `$ref`, `allOf`,
`oneOf`, `anyOf`, `type`, `enum` and `const`, a shape keyword written with no `type` claiming no
kind there ([`shape-applicator`](#shape-applicator)), values comparing as an `enum`'s entries do, so
that `1` and `1.0` are one, and the read takes in at most 256 schemas and 4096 `enum` members. An
`allOf` stays the composition it would otherwise be, with nothing lost, where:

1. the read does not finish: a cycle, either bound, a `$ref` landing on no schema, or a value it
   cannot convert;
2. it writes more than 64 conjuncts, which the election below compares pairwise;
3. a conjunct is `false` ([`allof-false-branch`](#allof-false-branch)), or nests past the flattening
   depth ([`allof-classification`](#allof-classification));
4. the conjuncts share no value, a null they all admit being no shared value unless a conjunct
   admits nothing else. A warning says so at the schema where their instance types share none
   ([`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts)), and otherwise at a branch writing a
   `type` that excludes an object ([`allof-inline-residue`](#allof-inline-residue)). Where neither
   holds, as for two `enum`s sharing no member, each branch is kept with an info and no report
   names the contradiction.
5. a `$dynamicRef` the position does not expand stands beside it: `{allOf: [{$ref: T}], $dynamicRef:
   '#m'}` is a `Model` keeping the `$dynamicRef`, even where `T` admits no object and its `$ref` is
   kept too.

A `$dynamicRef` a conjunct writes masks no kind: one the position expands is read as the schema it
names, and one it does not admits every kind. A union beside the `allOf` sharing no kind with the rest, as in
`{allOf: [{type: string}], anyOf: [{type: integer}, {type: boolean}]}`, is left out of the reading:
the position lowers as the rest does, the union is kept under `openapi:oneOf` or `openapi:anyOf`
(`degraded_lowering`) with an info, and the warning of
[`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts) names it at the schema, so no union is
distributed into models no instance satisfies ([`union-beside-structure`](#union-beside-structure)).

An `enum` or a `const` beside an `allOf` that refines is one of its conjuncts, the keywords beside
the `allOf` writing a value set, rather than its rival in the election of
[`competing-keywords`](#competing-keywords): `{enum: [a], allOf: [{$ref: Level}]}` elects its
principal from the same set as `{allOf: [{$ref: Level}, {enum: [a]}]}`, its branches fold and its
`$ref` still lowers. Beside an `allOf` that composes, the value set wins that election and the
`allOf` stays verbatim.

The principal is chosen from the whole set:

1. an inline value set, `enum` or `const`, within every other conjunct and more than a restatement of
   what a `$ref` fixes: it says which values the position holds, and what it narrows cannot;
2. else a `$ref`, since a narrower inline `type` replacing it would drop what its target says: of
   several, the one within the most conjuncts, a tie going to the least reference text, bytewise,
   then to the least text of the entry writing it, so no order written decides and
   `{allOf: [{$ref: Str}, {$ref: Uuid}]}` is a use of `Uuid` keeping the other `$ref` with an info.
   A `$ref` is no principal where the kind-inference policy reads its target as kinds leaving out
   one the position's declarations name, null aside, as the target then admits that kind whole, or
   where its target admits every kind beside a position declaring one or a conjunct naming one: a
   declared kind decides before an inference ([`policies`](#policies)) and before a target saying
   nothing of the kind, so `{allOf: [{$ref: E}, {type: string}]}` over `E: {items: …}` is the
   string, the `$ref` kept with an info naming the policy, `{type: [object, string], allOf: [{$ref:
   U}]}` over `U: {properties: …}` the union of an object and a string, and `{allOf: [{$ref: R},
   {type: string}]}` over `R: {required: [a]}` the string too, the info saying the target admits
   every kind, as does a union beside the `allOf` over `R`;
3. else, of the conjuncts naming a kind by `type`, `enum` or `const`, or by a union they write, the
   one within the most others, the keywords beside the `allOf` among them. A conjunct is within
   another where its kinds, its values, its `format` and its `items` are. Two formats of the table
   ([`string-formats`](#string-formats), [`numeric-formats`](#numeric-formats)) compare by what they
   select, a format narrows the bare kind it applies to, one the table does not list included,
   `items` narrow an `array` writing none, and two `items` compare by what each says, so `uuid` beats
   `string`, `email` beats `string` and a list of `uuid` beats a list of `string`, in either order,
   as a field redeclared the same ways does
   ([`allof-redeclared-property`](#allof-redeclared-property)).

No tie goes by the order the conjuncts are written in. Of conjuncts lying within as many others, or
of inline value sets holding the same values, a union goes before the words, which then bound it,
and a `oneOf` before an `anyOf`, as [`competing-keywords`](#competing-keywords) elects of one schema
writing both: `{allOf: [{anyOf: [{type: number}, {type: boolean}]}, {oneOf: [{type: string}, {type:
integer}]}]}` is a use of the `oneOf`, keeping the `anyOf`. Two unions of one keyword elect neither,
as no rule ranks their branches, and nor do words neither of which lies within the other: two
formats neither of which narrows the other, two `items` saying different things, or value sets that
only overlap. The position then lowers by the keywords beside the `allOf`, the top type where they
name no kind, keeping each entry with an info. Words admitting alike part only where a node reads a
keyword from its principal alone, `items`, `prefixItems`, `additionalItems` and the content
vocabulary: of those writing every such keyword another writes, and alike, one is elected, and
where none does, neither is. Where nothing tells them apart, the keywords beside the `allOf` stand,
else the entry whose text, its keys sorted, is the least. A schema written twice is one conjunct,
its first copy standing for both and the other saying nothing more, so the order written shows only
in the pointer of the node it lowers to.

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

A `oneOf` or `anyOf` written beside the `allOf` is one more conjunct, as one on a branch is: the
keywords beside the `allOf` are read with it, and name a kind where they write one. Where a branch
is then the principal, the position is a use of it and the union is a keyword beside the refinement.
The principal consumes it where it says it, as on a branch. Otherwise one whose branches declare no
shape against the position ([`union-beside-structure`](#union-beside-structure)) is kept under
`openapi:oneOf` or `openapi:anyOf` as `validation_only` and reported as an info
`openapi/validation-only-keyword` at the position, as beside any body, and any other is kept there
under `degraded_lowering`, with the report other keywords beside a refinement draw. So
`{allOf: [{type: string, maxLength: 10}], anyOf: [{type: string}, {type: number}]}` lowers as the
same union on a branch does, and `{allOf: [{$ref: Str}], oneOf: [{minLength: 1}, {maxLength: 2}]}`
is a use of `Str` keeping its `oneOf` as validation logic. Where the keywords beside the `allOf`
are the principal, the position lowers by them as if no `allOf` were written, a union among them
lowering as [`union`](#union) or [`union-beside-structure`](#union-beside-structure) lowers it there:
`{allOf: [{type: [string, number]}], anyOf: [{type: string, format: uuid}, {type: integer}]}` is the
union of a `uuid` and an `integer`, and beside an `allOf` no conjunct names a kind in, `{allOf:
[{multipleOf: 2}], anyOf: [{multipleOf: 3}, {multipleOf: 5}]}` is the `Union` its flat spelling is,
holding the bound in `Union.Constraints`. A use reads past such a refinement as past any, and past no
`allOf` a union beside it is distributed across or kept beside.

Over a target that is no object alone, `{allOf: [{$ref: T}], K}` lowers as 3.1's `{$ref: T, K}`
where `K` is annotations and value bounds. The two part on a structural keyword, which a `$ref` site keeps
verbatim reading no target ([`shape-applicator`](#shape-applicator)), and on `type: [T, "null"]`,
which the nullable-inference policy reads as widening beside a `$ref` and which conjoins here, as JSON
Schema says ([`nullable`](#nullable)). A `$ref` naming either parts as well: it reads what `K` says
here, and past it there, as a chain of `$ref`s is read to its end ([`ref-use-site`](#ref-use-site)).

The IR's rule that compilers leave flattening to emitters
([design rules](../ir/README.md#design-rules)) is not what this departs from, and the alternative of
keeping such an `allOf` a decorator for emitters to collapse is weighed in
[models](../ir/models.md#an-allof-no-object-satisfies).

- Cases: `allof-refinement`
- Codes: `openapi/degraded-construct`, `openapi/validation-only-keyword`
- Tests: `schema.TestRefinement_AnAnnotationOnlyBranchLandsTyped`,
  `schema.TestRefinement_PrimitiveBranchesLowerToTheirMeet`,
  `schema.TestRefinement_DisjointConjunctsStayAComposition`,
  `schema.TestRefinement_NamesItsTargetAtEveryPosition`,
  `schema.TestRefinement_KeywordsLandAtEveryPosition`,
  `schema.TestRefinement_KeptKeywordsLandAtEveryPosition`,
  `schema.TestRefinement_SeveralRefsUseTheNarrowest`, `schema.TestRefinement_KindsBeyondAnObjectAreAUse`,
  `schema.TestRefinement_ADynamicRefIsReadAsWhatItNames`,
  `e2e.TestAllOfCompose_ATargetAdmittingEveryKindIsUsedNotComposed`,
  `e2e.TestAllOfCompose_AModelKeywordOnAKindThatIsNoObjectIsKept`,
  `e2e.TestAllOfCompose_EntriesClaimingTwoKindsAreNotAModel`,
  `e2e.TestAllOfCompose_SeveralRefsThatAreNoObjectUseTheNarrower`,
  `e2e.TestAllOfCompose_ASelfReferenceStaysACompositionWithItsCycleFinding`,
  `e2e.TestAllOfCompose_AFormBodyOfAStringBesideMembersIsAString`,
  `e2e.TestAllOfCompose_ATargetNamingAKindWithAnUnexpandedDynamicRefIsUsed`,
  `e2e.TestAllOfCompose_APickAmongSeveralReadsNoOrder`, `e2e.TestAllOfCompose_APositionUsedTwiceLowersAsOnce`,
  `e2e.TestAllOfRefine_AKeywordOfAnotherKindIsKeptOnWhatTheTargetDeclares`,
  `e2e.TestAllOfRefine_ABoundOverATopTargetLandsOnTheUse`,
  `e2e.TestAllOfUnion_ABoundBesideAnAllOfNamingNoKindIsHeldByTheUnion`,
  `e2e.TestAllOfUnion_AUnionSharingNoKindWithTheRestIsKeptNotDistributed`,
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
  `schema.TestRefinement_ATieOfKindsNeitherWithinTheOtherElectsNone`,
  `schema.TestElect_ComparesEachPairOnce`,
  `e2e.TestElection_BothKeywordsElectTheOneOfWhicheverIsWrittenFirst`,
  `e2e.TestElection_TwoUnionsOfOneKeywordElectNeither`,
  `e2e.TestElection_TwoAnyOfEntriesThatDeclareNoShapeAreKeptWhole`,
  `e2e.TestElection_ARefIsThePrincipalOverAUnionWhichSaysNothingMoreOfIt`,
  `e2e.TestElection_ARefTieGoesByReferenceText`, `e2e.TestElection_TwoRefsWrittenAlikeGoByTheirEntries`,
  `e2e.TestElection_TwoInlineEntriesThatDisagreeOnItemsElectNeither`,
  `e2e.TestElection_TwoInlineEntriesTieOnTheOneWritingItems`,
  `e2e.TestElection_AUnionTyingWithTheWordsIsThePrincipal`,
  `e2e.TestElection_ASchemaWrittenTwiceIsOneConjunct`,
  `e2e.TestElection_WordsAdmittingAlikePartOnWhatTheirNodeReads`,
  `e2e.TestElection_ValueSetsAlikeGoByTheirText`,
  `e2e.TestAllOfRefine_ATargetInferredNarrowerThanTheDeclaredKindsIsNoPrincipal`,
  `e2e.TestAllOfUnion_ARefAdmittingEveryKindBesideAUnionIsKeptAsTheInlineSpellingIs`,
  `e2e.TestAllOfRefine_ARefLosingATieIsReportedAsATie`,
  `schema.TestRefinement_ANoKindConjunctIsNeverThePrincipal`,
  `e2e.TestKindInference_ADeclaredKindOutvotesARefTheKeywordsClaim`,
  `e2e.TestKindInference_AUseOfAnInferredKindIsAnAliasOfIt`,
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
  `schema.TestRefinement_AUnionBranchIsAConjunct`, `schema.TestRefinement_AUnionBesideTheAllOfIsAConjunct`,
  `schema.TestRefinement_AUnionThePrincipalSaysIsConsumed`,
  `schema.TestRefinement_TheKeywordsBesideWithAUnionLowerAsWithNoAllOf`,
  `schema.TestRefinement_AShapelessUnionBesideIsValidationLogic`,
  `schema.TestUse_AUnionBesideAnAllOfReadsThroughOnlyARefinement`,
  `e2e.TestExternalContent_ARefinementReadsACopyInAnotherDocumentApart`
- Status: partly built: a structural keyword beside a 3.1 `$ref` keeps what the same keyword here
  consumes (#1067), and a `$dynamicRef` beside the `allOf` keeps a `Model` over a target admitting no
  object (#2009).

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
`object`, which the composed `Model` already restates, its `additionalProperties` and
`patternProperties` where it is the schema object the model takes its catch-all from
([`additional-properties`](#additional-properties)), for an entry flattened under
[`allof-classification`](#allof-classification), its `allOf`, and the `oneOf` or `anyOf` the
position lowers, distributing it across its variants or composing it as a mixin, with the
`discriminator` the branch writes beside it, which routes it
([`union-beside-structure`](#union-beside-structure)).

JSON Schema conjoins an `allOf`'s keywords with those beside it, so what a branch writes for the
position reads as written beside the `allOf`, in a composition and in a refinement
([`allof-refinement`](#allof-refinement)) alike: its `title`, `description`, `externalDocs`,
`deprecated`, `example`, `examples`, `xml`, `default`, `readOnly`, `writeOnly`, value constraints,
collection bounds, `format: password` ([`password`](#password)), vendor extensions and
validation-only keywords ([`validation-only`](#validation-only)) land where the same keyword beside
the `allOf` lands, each read at the branch's own pointer and reported as that keyword is there,
which for most is not at all. A `nullable` folds only from a branch writing no `properties`, under
the nullable-inference policy ([`nullable`](#nullable)). Neither `unevaluatedProperties` nor
`unevaluatedItems` folds, in any form: each applies to what no keyword beside it evaluates (JSON
Schema 2020-12 §11.2, §11.3), so on a branch it holds the instance to what that branch alone
evaluates, where beside the `allOf` it would exempt what the position and the other branches
evaluate, and the branch kept whole is its record. A bound does not fold where the position has no
field for it, as one beside the `allOf` reaches none ([`constraints`](#constraints)): a value or
collection bound no type the position admits takes, the types of the schemas it names among them,
one no type its own branch declares takes, since such a branch
admits no value it bounds, or one on a node with no `Constraints` for one, the `Union` of a type
list holding one where a variant whose type takes it does, a value bound but a property count on a
`Model` or a `MapT`, and a collection bound on any node but a `List` or a `Tuple`, that `Union`
holding one where such a variant does; a collection bound
`Constraints` cannot state, a `uniqueItems` of `false` or a count past 64 bits, never folds. A
`$dynamicRef` beside the `allOf` keeps it from refining ([`allof-refinement`](#allof-refinement)),
where a union beside it is one more conjunct, and neither changes anything else here: where the
`allOf` composes, its branches fold into the composition as they do with neither there, beside a
`$dynamicRef` it keeps from expanding ([`dynamic-ref`](#dynamic-ref)) or a union kept verbatim
([`union-beside-structure`](#union-beside-structure)). Where a union beside the `allOf` or on one of
its branches distributes, a branch's annotations and what a use binds reach the `Union` and its
carriers, and a value bound each variant, as the same keyword beside the `allOf` does, and a branch
with no residue is kept on no variant
([models](../ir/models.md#composition-beside-a-union)).

Folding needs no precedence, because a branch that disagrees with any conjunct on one of these, the
keywords beside the `allOf` among them, folds nothing, in every order: the keywords beside the
`allOf` stand, and each branch writing that keyword otherwise stays whole, with its report. Two
keywords a position keeps in one entry, `if` and `then` say, are compared as that entry. A
`deprecated`, `readOnly` or `writeOnly` written `false` disagrees with nothing and lends nothing, as
it states nothing a `true` elsewhere does not ([`ref-use-site`](#ref-use-site)). Value
bounds and the counts `minItems` and `maxItems` agree where they meet, as a redeclared field's do
([`allof-redeclared-property`](#allof-redeclared-property)): `maxLength: 3` beside `maxLength: 5`
folds to `3`, `minItems: 1` beside `minItems: 2` to `2`, and `multipleOf: 4` beside `multipleOf: 6`
to `12`, whatever their order; a number written two ways keeps its first spelling here. Two `pattern`s
have no one value, so each disagrees with another written differently. A writer stating no value
parts no other: a 3.0 `exclusiveMinimum` of `false`, or of `true` with no bound beside it, which
fills no field, and a bound the rule table refuses ([`validation`](#validation)) say nothing a value
must meet, so its branch folds where nothing else parts it, and what the writer wrote is read and
reported at its own pointer, and kept where the same keyword beside the `allOf` is. A branch whose
folding keywords write a relative `$ref` it reads against another base than the position does, an
`$id` at or above it naming another URI, folds nothing either, since that `$ref` would name another
schema read at the position; it stays whole, with its report, and is no conjunct the others are
compared with. One writing none
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
the source contradicts. A position composes beside such a branch only where the conjuncts share no
value but null, as `{allOf: [{type: [object, "null"], …}, {type: [string, "null"], maxLength: 3}]}`
do, or where its read is cut short ([`allof-refinement`](#allof-refinement)): beside conjuncts
admitting a string, `{allOf: [{$ref: Pet}, {type: string, maxLength: 3}]}` over a `Pet` writing no
`type` is a use of the string. Where the conjuncts share no value, as they do when `Pet` writes
`type: object`, [`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts) reports that once at the
schema and the branch's report is an info. Two branch kinds are outside this
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
- Tests: `schema.TestVoices_AFalseFlagAgreesWithATrue`,
  `schema.TestAllOf_BranchWrittenByReferenceStillDerivesResidue`,
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
  `schema.TestVoices_ARefusedBoundPartsNoWriter`,
  `schema.TestAgreement_ReadsABoundInTheDialectOfItsSchemas`,
  `schema.TestVoices_KeywordsOneEntryHoldsDisagreeAsOne`,
  `schema.TestVoices_AnUnevaluatedKeywordStaysWithItsBranch`,
  `schema.TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode`,
  `schema.TestVoices_KeptAndMetBranchesHoldAtEveryOutcome`,
  `schema.TestAllOf_BranchResidueRidesEveryDistributedVariant`,
  `schema.TestAllOf_ABranchReadAgainstAnotherBaseFoldsNothing`,
  `schema.TestAllOf_ABranchWritingNoRelativeReferenceFoldsUnderAnyBase`,
  `schema.TestRefinement_ABranchReadAgainstAnotherBaseFoldsNothing`,
  `schema.TestCollectionBound_OnABranchLandsAsItsFlatSpelling`,
  `schema.TestCollectionBound_OnABranchMeetsInEveryOrder`,
  `schema.TestCollectionBound_OnABranchWithNoFieldStaysWithItsBranch`,
  `schema.TestRefinement_AValueBoundOnABranchBesideATypeListLandsOnItsVariant`,
  `schema.TestRefinement_ABranchBoundBesideATypeListIsReadAtTheBranch`,
  `schema.TestAllOf_ABranchBoundTheTypeCannotTakeStaysWithIt`,
  `schema.TestConstraints_ABranchReadsTheBoundsItsOwnTypeTakes`,
  `schema.TestConstraints_ABranchCountItsTypeCannotTakeIsKeptWithIt`,
  `openapicases.TestCore_SpellingsHoldTheRecord`

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
and anywhere else nothing but where it was declared. Two such types lose to neither: what else each
says decides, so an alias of one is narrower than the other it stands for, and two aliases neither
standing for the other are not compared. Where `{}` loses, the winner's own null
admission stands alone, since `{}` admits null yet lowers non-nullable. Two bare primitives compare
by value domain and spelling (`integer` against `number`, `uuid` against `string`, `int32` against
`int64`). A `Scalar` or `Enum` against a bare primitive wins over a kind it equals or narrows
(`string` against `format: byte`, or an unknown format over bare `string`), is disjoint from a kind
of another family, or from another string format where neither is `bytes` (`uuid` against
`format: date`), and overlaps any other kind (`uuid` against `format: byte`); for an `Enum`, that
overlap is disjoint where the kind's judge refuses every member, an enum behind an alias, as a
member written beside a bound is, judged by the members of the enum it stands for, which the alias
can only narrow. A `Scalar`'s kind is that of the values it travels as: its base's, but for a base
its `Encoding`'s `WireType` does not spell, whose values travel as that type, so `format: unixtime`
on an `integer` is narrower than `integer`, overlaps `int64` and is disjoint from `string`, while
`byte` stays `bytes`, which a `string` spells. A string format whose text an `Encoding` names
otherwise, as `http-date` and `contentEncoding` do, has no judge, as `bytes` has none, since the
format's own grammar reads another text: an enum of HTTP-dates beside `format: http-date` overlaps
it. A `Scalar` hoisted for `time`,
whose grammar, RFC 3339's `full-time`, the compiler holds though the table pairs it with no kind
([`string-formats`](#string-formats)), is read wherever a kind is, through an alias too, as a
string format of its own: within `string`, an overlap with `bytes`, and disjoint from every other
kind, `time-local` and `date-time` among them, and from an `Enum` none of whose members it
admits. An enum with no members is refused
vacuously, so it is disjoint from any kind it would otherwise overlap, `bytes` included; an enum
with members against `bytes` stays an overlap. A `const`, or an alias of one, wins when the kind
admits its value; where it does not, it is disjoint from a kind with a judge and overlaps `bytes`,
which has none. Against a
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
Two `const`s of different values share none, so they are disjoint. Two enums of one closedness,
bit-flag reading and fallback, whose members holding one value say the same of it, are judged by
the values they hold: sharing none, they are disjoint, whatever their value types. Of two value
types one within the other, the narrower says more (`integer` against `number`), so one whose
values the other holds every one of, of the same value type or the narrower, is narrower where it
also says all the other does beside its members, and otherwise the two meet, as do two each holding
a value the other lacks, in an enum of the values they share, of the narrower value type (below),
where both list those values in one order. Two listing them in different orders overlap, since an
enum of both would take its order from one declaration. Two holding one set of values of one value
type, or in another order, or saying the rest differently, two of value types neither within the
other (`int32` against `uint32`), and any other pair of enums, are judged by their kind.
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
An alias is narrower than the type it stands for, whatever its kind: a reference to it reads all
that type says, and it may say more. One whose chain admits null, as `{anyOf: [E, {type: null}]}`
does, is not, since `E` refuses null: where the chain adds nothing else, docs included, `E` is
narrower and the field holds it, and otherwise the two are judged as below. Against any other type, an alias whose chain ends in an enum,
a `const`, a `Scalar`, a list, a map or a tuple is judged, and a node it meets built, as that node
holding the bounds the chain and the node say together and each detail the nearest of them sets,
documentation field by field, as a reference to the alias reads it
([annotations](../ir/annotations.md#what-a-reference-carries-down)). So `{$ref: E, description: d}`
and an `allOf` of `E` alone each meet an enum beside them as `E` would, holding what they add, and
a title over an alias holding a description keeps both. Two aliases of one node are read only down
to it, so they are judged as two `Scalar`s of one base are, and meet in an alias of it. A chain
giving an encoding, bounds no one value states, bounds that a value an enum or a `const` lists fails
or that nothing here judges, such as a `pattern` on one, any bound on an open enum, which bounds the
values it does not list too, or keeping a relative `$ref` under `Unmodeled` is judged as it is
written. Whatever the arm, a type admitting null through its alias chain is narrower than no type
whose reference refuses null, since a field holding it would admit null all the same: the two
overlap. The top type, which loses before any arm, refuses no null whatever its reference says.
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
one it says it is: a model than what it composes through `Base` or `Mixins`, read through aliases,
up to 64 types for one question. A model, a map or a
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
and a kind, a union and a type neither holds, or an external and anything else. A type within a
variant of an untagged union is narrower than the union, as is a closed untagged union every
variant of which is within a type than that type, since an open one admits a variant it does not
list; of a `oneOf`, the type must be within one variant alone and disjoint from every other, since
a `oneOf` refuses a value two variants share. A tagged union holds neither: a discriminator routes
by a tag a variant's values need not agree with. Nor is a type within a union holding bounds of its
own, or reached through an alias that holds some, since each bounds the values of its kind
whichever variant holds them, which the variants do not say. One judgment reads at most 64
variants, past which the two are unrelated. A structure whose source admits every instance of the
other's kind is the exception to the disjoint pairs, as `properties` alone constrains objects and
admits a string, and `{allOf: [{$ref: ObjOrInt}], properties: …}` over `{anyOf: [{type: object},
{type: integer}]}` admits every integer: what it admits is read as
[`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts) reads a conjunct, through its `type`, its
`enum` and `const`, its `allOf`, `oneOf` and `anyOf`, the keywords beside a `$ref` and what each
`$ref` lands on, and for a variant of a union, a distributed one among them, what the schema writing
the union admits too, and each schema holding that one in turn as an `allOf` entry or a union
branch, up to the first that is neither: a union written in an `allOf` entry of `{type: object}`
admits no array, however deep the entry. A schema read as an `allOf` entry and no union's branch is
read alone. It admits a kind whole only where no keyword it takes in refuses some instance
of that kind while admitting others: a value set, a numeric or a string bound, a `format`, and a
keyword applying to arrays or to objects alone, `items` or `required` among them, each admit their
kinds in part, but for one whose schemas, applied to the instance's parts, each state nothing, as
`items: {}` and `additionalProperties: true` do; and a union admits a kind whole where a branch does
and, for a `oneOf`, which refuses what two branches admit, no other branch admits it. So `{minimum: 5}`, `{format: int32}`,
`{enum: [1, 2]}` and `{anyOf: [{type: object}, {type: integer, minimum: 5}]}` each admit some
integers and not every one, and a structure composing one of them is judged against `integer` as a
structure stating its type is: the conflict is reported and the declaration the field does not keep
is kept verbatim, since a field of either type alone would lose what the other says. Where that read
meets what it does not follow but which could refuse a value of
some kind, a `not`, an `if` or a `dependentSchemas` holding a schema, a `$dynamicRef` or a
`$recursiveRef` the bindings read as no `$ref`, a cycle of conjuncts or a `$ref` landing on no
schema, the structure reads as stating its type; a `not` or an `if` written `null` is the keyword
left out ([`null-schema`](#null-schema)) and refuses nothing. A value type or a
structure of another instance type is narrower than it, in either order, where it and each alias
naming it hold no more than an alias of `{}` may to be read as `{}`, and overlaps it otherwise,
since a type replacing it would lose what it holds. So a structure written at the declaration
itself, which keeps its docs, deprecation and redaction on its own node, overlaps a value type or a
structure of another instance type where it holds any of them; one a `$ref` reached carries them
down to the field. Two such structures of different instance types overlap, sharing every value of
a third. The source is read in the document holding the composing model, so a structure another
document declares reads as stating its type. A pair gets the same verdict as a field's types and as two containers' members,
but for tuples, above.

The field takes the type of the declaration whose type is the same as, narrower than, or meets the
most declarations' types. Of those holding as many, the later wins, where a spec writes the
specialization, as the flattening reads it ([models](../ir/models.md#the-flattening)): a failure
response composing a success envelope and then a failure schema takes the failure's `success`. Of
declarations of one type at distinct nodes, as copies of one schema are, the node whose ID sorts
first stands for the rest, so no order decides which the field names. A declaration's type within
every other's is the intersection, and it wins whatever the order. Each declaration whose type the
winner's is the same as or narrower than, or meets, folds into the field, which adopts its shape
detail; every other one is dropped. So `int32`, `uint32` and `uint16` fold to `uint16` with nothing
dropped, in every order, while `int32` and `uint32` alone keep the later declared and report the
other.

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
meeting the winner give one detail differently, the later's is kept and the earlier reported and
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
only `$ref`, or else the first `$ref` whose type takes part in a discriminator hierarchy, is its
`Base`, and the rest are `Mixins` in declaration order. The fold reads the hierarchy off the types:
a `Model` or a `Union` holding a discriminator its source declares, or a `Model` one of whose
supertypes does, at any depth. So a field declared as `Meeting` and as an inline object
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
last written is kept, and a declaration giving another is reported and kept; two values are one
where they are one JSON value, a number by its magnitude and an object by its members in any order.
Values are refused before one is kept, so a value the field takes is never lost to a refused one
written after it.

Constraint keywords fold over the declarations folded into the field, each taking the value that
says what they all do, so no order decides it. A declaration's are those its property holds and,
where the fold composes the field's model, those of the node it owns, which its property leaves to
that node ([`constraints`](#constraints)): the composed model stands at no position of its own and
holds none, so the field holds them. Where the field's type is one declaration's node, or one the
merge builds holding the bounds the nodes say together, the bounds stay on that node. `minimum`, `exclusiveMinimum`, `minLength`,
`minItems` and `minProperties` take their largest value and `maximum`, `exclusiveMaximum`,
`maxLength`, `maxItems` and `maxProperties` their smallest, by magnitude, a number written two ways
keeping the last spelling; `uniqueItems` holds where any declaration sets it. A bound and its
exclusive form are two keywords, each folding alone, and both apply. `multipleOf` takes whichever
value is a multiple of the other, and otherwise their least common multiple, a number no
declaration wrote, whose multiples are exactly those both admit. `pattern`, `patternMessage`,
`precision` and `scale` have no order and no one value saying two of them, so the last declaration
to set one keeps it, as does `multipleOf` for two steps past 1,024 significant digits between them
or a multiple past what a `BigVal` holds. The title, description, external links, deprecation, XML
hints and each `Unmodeled` key fold over every declaration, the last to give one keeping it, and a
declaration giving one differently is reported and kept; of two keeping one `Unmodeled` entry alike,
the one written first is the field's, in the document listed first and then at the pointer sorting
first, since two files each holding one schema write the same pointers. An entry two declarations
write alike is one where it writes no relative `$ref`, or both read it against one base: under an
`$id` naming another URI, a `$ref` spelled alike names another schema.

A field keeping the first declaration's identity keeps that declaration's pointer, and either way it
names the heuristic that promoted its deprecation or its redaction from an extension, wherever the
one it keeps came from, and no other. So a field names one source. `Provenance` holds one pointer, and what a
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
structure stating its instance type or admitting the scalar's kind only in part, two structures of different instance types each stating
theirs, or two lists or maps whose members share no
value where one is never empty. Types that share values but that the merge does not narrow to one
(`int32` against `uint32`, an enum of `integer` against an `int32` that admits one of its members,
a `const` against a kind with no judge, against a node that ends in a kind, but for the `time` node
above, or against a `const` of its value saying more, two `Scalar`s of different encodings, such as
`format: hex` against `format: byte`, which no one node holds, two nodes saying one set of values
differently, two enums listing the values they share in different orders, two enums of value types
neither within the other sharing a value, two nodes giving one
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

A member a model's composition declares more than once folds the same way, wherever the declarations
are written, so a `$ref` entry and the schema it names written inline in its place fold alike, but
for a tie inside the entry: an entry brings its member already folded, so a declaration that lost a
tie there no longer counts toward the type holding the most declarations, as it does where the
entry's schema is written inline. The flattening ([models](../ir/models.md#the-flattening)) would
otherwise take the model's own declaration over an entry's, and of two entries the later, and judge
none. Once every lowering has returned, one settlement folds, on each model, every member its own
declarations or its `allOf` entries decide more than once, asking the run's context before each
model. Each entry brings the declaration the flattening of what it names takes, which a model
settled first holds folded from every other its composition gives the member, and one another
entry's composition holds, however deep, stands within that entry's, so it brings nothing. A
declaration an entry brings is judged where its type is written, as an inline one is: a member a
part settled keeps its first declaration's identity whichever declaration gave its type, so the part
records where the nodes of that type are written: at the declaration whose node the member keeps,
or, for a node the fold built, where it stands and at the declaration writing each node it holds, of
which it reads 128 at most. A node written at none of them reads as a schema a `$ref` reached. A
value a declaration gives is read against the field's type the same way, at each place the type's
nodes are written, which for a node the fold built is not the winning declaration's alone. Each
model settles after every model it composes, however deep a hierarchy nests, so the order components
are declared in decides nothing, nor, off a cycle, their names. A cycle of compositions reads the
model it returns to as it stands, and the models' IDs pick which one that is, so where two
declarations on a cycle admit no value together, the names decide which its models keep, and the tie
is reported.
The declarations are read in the order the source writes them, a `$ref` entry's where the entry is
written and the schema's own `properties` after its `allOf`; a union branch distributed into a
variant reads before the body. A declaration an entry brings stays declared on its own model, so it
is not kept verbatim, and a report about it sits at the entry bringing it, naming the member in its
`Provenance.Node`, and names a declaration another document holds with that document's name, a
member a part settled by the one place its type is written, or by its first declaration where its
type is written at several places or none, and one that would so be named where nothing is written,
as a member only a part's entries declare, by the entry bringing it. A member the model declares
itself keeps the identity of the first of its own declarations, unless another model holds that
declaration too, as a node interned at the `allOf` branch writing it does, or as the variants of a
distributed union share their body's; that one, and one only entries declare, the model declares in
the `composed` namespace at `<model>/allOf/properties/<name>`, where no schema is written, so it,
and a node its fold mints, name an overlay that wrote every entry and declaration it is assembled
from. A model the merge composes, a distributed variant or a field's composition, shares its pointer
with the schema written there, which may settle a member of that name too, so it declares one an
`allOf` deeper, at `<model>/allOf/allOf/properties/<name>`, and no two models name one member. The
member is required where the model's `required` names it, which the name then leaves, and a
discriminator naming the model's own declaration, or by its name a member only entries declare,
names the member ([`discriminator`](#discriminator)).
The registry does not record which heuristic made a brought declaration secret, so a secret one's
`Provenance.Inferred` marker comes with its redaction, as a deprecation's does. A model the fold
composes for a member lists its parts as the entries bringing them are written, and settles in turn,
through 8 rounds; one nested deeper is left as its entries declare it, with an
`openapi/budget-exceeded` error. What a model's parts bring is read only for the names more than one
model declares, so a hierarchy costs its depth again at each level only where every level adds such
a name, or composes a model redeclaring one it holds, which telling whether that declaration stands
within the hierarchy reads to its base. A run reads at most 16,777,216 of their declarations, a read
of a model not settled yet, as a cycle's are, counting each type and entry its flattening walks too,
as does telling whether one part's declaration stands within another's; past that each model left
folds its own declarations alone, with an `openapi/budget-exceeded` error at the first.

- Cases: `allof-conflicting-type`, `allof-narrowing-redeclaration`, `allof-redeclared-models`,
  `allof-redeclared-bounds`
- Codes: `openapi/conflicting-redeclaration`, `openapi/degraded-construct`, `openapi/budget-exceeded`
- Tests: `merge.TestFold_NullabilityIntersectsWhenTargetsAgree`,
  `merge.TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict`,
  `merge.TestFold_ALosingDetailIsKeptAndReported`,
  `merge.TestFold_TitleAndExternalDocsFoldLikeDescription`,
  `merge.TestFold_ANarrowingIntersectsNullability`,
  `merge.TestFold_ADroppedTargetKeepsTheWinnersNullAdmission`,
  `merge.TestFold_TheJudgmentsVerdictIsWhatIsReported`,
  `merge.TestFold_RedeclaredModelsCompose`,
  `merge.TestFold_OnlyTwoOrMoreModelsCompose`,
  `merge.TestFold_AComposedFieldIntersectsNull`,
  `merge.TestFold_AComposedFieldKeepsEveryDeclarationsDetail`,
  `merge.TestFold_AComposedFieldHoldsTheBoundsOfEachDeclarationsOwnNode`,
  `merge.TestOwnBounds_ReadsTheNodeADeclarationOwns`,
  `e2e.TestCompile_AMemberTheRedeclaredPropertyFoldComposesKeepsItsBounds`,
  `merge.TestFold_AComposedFieldAdoptsOnlyANull`,
  `merge.TestFold_ComposesOnlyTheDeclarationsItJudges`,
  `merge.TestFieldSpan_NamesEachSetOfDeclarations`,
  `merge.TestFold_AFieldAnotherModelCarriesTakesAnIdentityOfItsOwn`,
  `merge.TestPropIndex_ADiscriminatorFollowsItsFieldToANewIdentity`,
  `e2e.TestAllOf_ACollisionPastTheFlatteningDepthIsRefused`,
  `merge.TestSelectBase_ChoosesAsTheRuleSays`,
  `schema.TestAllOf_AFieldRedeclaredAsTwoModelsComposesThem`,
  `schema.TestAllOf_AComposedFieldKeepsADiscriminatedBase`,
  `schema.TestAllOf_AComposedFieldElectsTheBaseAWrittenAllOfDoes`,
  `schema.TestAllOf_EveryDistributedVariantSharesOneComposedField`,
  `schema.TestAllOf_AnAliasIsJudgedAsTheTypeItStandsFor`,
  `schema.TestAllOf_RelatedStructuresAreNotCalledAConflict`,
  `schema.TestAllOf_ATypeHoldingAnotherTakesTheField`,
  `schema.TestAllOf_AnObjectSayingMoreThanItsTypeComposes`,
  `schema.TestAllOf_AnAliasOfTheTopTypeAddsNoConjunct`,
  `schema.TestAllOf_AStructureStatingNoTypeAdmitsEveryOtherKind`,
  `schema.TestAllOf_AnUntypedStructureKeepsWhatItsDeclarationWrites`,
  `schema.TestAllOf_AnUntypedReadPassingOverAKeywordKeepsTheConflict`,
  `schema.TestAllOf_ANullNotOrIfIsTheKeywordLeftOut`,
  `schema.TestAllOf_ADistributedVariantIsReadWithItsUnion`,
  `e2e.TestCompile_AFieldRedeclaredAsAStructureAdmittingItsKindIsNotAConflict`,
  `e2e.TestCompile_AStructureAdmittingPartOfAKindConflictsWithIt`,
  `e2e.TestCompile_AVariantIsReadWithEverySchemaHoldingItsUnion`,
  `schema.TestVariantHolders_ClimbsEverySchemaHoldingAUnionBranch`,
  `schema.TestAdmissionOf_PartialIsWhatAKeywordRefusesInPart`,
  `e2e.TestCompile_ANullNotBesideAUnionIsTheKeywordLeftOut`,
  `schema.TestAllOf_ASourceIsReadOnlyInItsOwnDocument`,
  `merge.TestTypeJudge_ModelsAlikeAreCopies`,
  `merge.TestTypeJudge_ModelsKeepingAReferenceAreAlikeUnderOneBase`,
  `merge.TestFold_ModelsAlikeAreOneType`,
  `merge.TestTypeJudge_ModelPairsStopAtTheirBudget`,
  `merge.TestTypeJudge_NestedModelsCostTheirBudgetNotTheirWidthToTheDepth`,
  `schema.TestAllOf_ModelsAlikeAreOneType`,
  `schema.TestAllOf_AModelComposesWithAMapOrAUnion`,
  `merge.TestTypeJudge_UnionVariantsStopAtTheirBudget`,
  `schema.TestMembers_AUnionBoundedBesideItIsNotTakenForAVariant`,
  `merge.TestPropIndex_PlacesADeclarationAtTheEntryBringingIt`,
  `merge.TestPropIndex_SettlesAMemberItsEntriesDeclareApart`,
  `merge.TestPropIndex_ReadsAnOwnDeclarationWhereTheWalkMetIt`,
  `merge.TestPropIndex_SettlesTheMembersOnlyEntriesDeclareByWireName`,
  `merge.TestTypeJudge_AUnionsVariantIsJudgedAtTheUnionsSite`,
  `merge.TestPropIndex_APromotedRedactionBringsItsMarkerInEitherOrder`,
  `merge.TestPropIndex_KeepsTheIdentityOfTheDeclarationItHoldsAlone`,
  `merge.TestFold_NamesADeclarationAnotherDocumentHolds`,
  `merge.TestTakenOutside_LeavesOutTheComposingModelsOwn`,
  `e2e.TestExternalContent_AMemberAnotherDocumentBringsIsNamedWithIt`,
  `schema.TestMembers_TwoEntriesDeclaringAMemberApartDeclareItOnTheComposition`,
  `schema.TestMembers_ARefAndTheSchemaItNamesWrittenInlineFoldAlike`,
  `schema.TestMembers_ATieAnEntrySettledIsNotCountedAgain`,
  `schema.TestMembers_AMemberAPartSettledIsJudgedWhereItsTypeIsWritten`,
  `schema.TestMembers_AMemberAPartMeetsIsJudgedAtEachDeclarationItHolds`,
  `schema.TestMembers_AMemberOnlyEntriesDeclareIsJudgedWhereItsTypeIsWritten`,
  `schema.TestMembers_ABroughtMembersDefaultIsItsOwnTypesWhereThatIsWritten`,
  `schema.TestMembers_ASettledMembersInlineTypeIsNoBaseOfWhatComposesIt`,
  `schema.TestMembers_AReportNamesABroughtMemberWhereItSaysWhatItDoes`,
  `schema.TestMembers_ADeclarationASettledModelHoldsKeepsWhereItsTypeIsWritten`,
  `schema.TestMembers_TheSettlementAsksItsContextBeforeEachModel`,
  `schema.TestAllOf_ADefaultIsItsOwnTypesWhicheverDeclarationTheNodeCopies`,
  `merge.TestTypeSites_NamesWhereAFoldedTypesNodesAreWritten`,
  `merge.TestBuiltSites_ReadsNoMoreNodesThanItsBound`,
  `merge.TestHoistedAt_ReadsEachPlaceADeclarationHoldsItsType`,
  `merge.TestFold_NamesABroughtDeclarationWhereItSaysWhatItDoes`,
  `schema.TestMembers_RequiresAndTheDiscriminatorFollowTheDeclaredMember`,
  `schema.TestMembers_AModelIsSettledAfterWhatItComposes`,
  `schema.TestMembers_ACycleSettlesTheSameInEitherOrder`,
  `schema.TestMembers_AModelAFoldComposesIsSettledInTheNextRound`,
  `schema.TestMembers_AModelTheMergeComposesNamesItsMembersApart`,
  `schema.TestMembers_CompositionsNestedPastTheRoundsAreReported`,
  `schema.TestMembers_AModelSettlesOnceHoweverManyRoundsReachIt`,
  `schema.TestMembers_AHierarchyOfAnyDepthSettlesWhateverItsModelsAreNamed`,
  `schema.TestMembers_ReadsGrowWithAHierarchysSizeNotItsDepthSquared`,
  `schema.TestMembers_ReadsPastTheBudgetFoldTheirOwnDeclarationsAlone`,
  `schema.TestMembers_ReadsAPartThroughItsAliasesAndNothingElse`,
  `schema.TestMembers_ReadingAModelNotSettledChargesItsComposition`,
  `schema.TestMembers_ACycleIsChargedTheCompositionsItsReadsWalk`,
  `merge.TestOwnership_ReadsEachModelsCompositionOnce`,
  `merge.TestOwnership_LeavesOutADeclarationAnotherComposesAtAnyDepth`,
  `merge.TestOwnership_StopsOnceItReachesEveryOtherModel`,
  `merge.TestOwnership_AWalkReturningToItsModelReadsOnToTheOthers`,
  `merge.TestOwnership_ChargesWhatItReadsAndLeavesInWhatACutWalkMisses`,
  `schema.TestMembers_ABaseComposedBesideItsHierarchyBringsNothingAtAnyDepth`,
  `schema.TestMembers_ACutTellingDeclarationsApartIsReported`,
  `schema.TestMembers_ACutTellingDeclarationsApartFallsAlikeInEveryRun`,
  `merge.TestOwnership_ReadsPastAPartNamingNoType`,
  `merge.TestPropIndex_ADiscriminatorNamingAnAppendedMemberNamesItsID`,
  `e2e.TestCompile_OverlayIsCreditedWithAMemberOnlyItsEntriesDeclare`,
  `merge.TestFold_AnEntryAlikeInTwoDocumentsKeepsOneWhateverTheOrder`,
  `operation.TestPartEncodings_KeyedOnTheMemberTheBodyDeclares`,
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
  `merge.TestFold_EnumsOfDifferentValueTypesAreJudgedByTheirValues`,
  `merge.TestFold_ThreeEnumsMeetInTheNarrowestValueType`,
  `schema.TestAllOf_EnumsOfDifferentValueTypesAreJudgedByTheirValues`,
  `merge.TestView_ReadsAnAliasAsTheNodeItStandsFor`,
  `merge.TestView_StopsAtTheNodeTheChainsShare`,
  `merge.TestSharedLink_NamesTheFirstNodeEveryChainNames`,
  `merge.TestAliasedNode_ReadsAChainUpToItsBound`,
  `merge.TestStandsFor_ReadsOnlyAnAliasChain`,
  `merge.TestFold_AnAliasMeetsAsTheNodeItStandsFor`,
  `merge.TestFold_AnAliasIsWithinTheNodeItStandsFor`,
  `schema.TestAllOf_AnAliasMeetsAsTheNodeItStandsFor`,
  `schema.TestAllOf_AMemberBehindAnAliasIsJudgedByWhatItStandsFor`,
  `schema.TestAllOf_AnAliasIsJudgedByWhatItAdds`,
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
  `merge.TestFold_ATieGoesToTheLaterAndIsReported`,
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
  `merge.TestFold_UnmodeledKeepsTheLastOfAKeyAndReportsAnother`,
  `merge.TestFold_AValueSpelledTwoWaysIsOneValue`,
  `merge.TestFold_ThePromotionMarkerFollowsTheDeprecationKept`,
  `merge.TestFold_ThePromotionMarkerFollowsTheSecretTaken`,
  `schema.TestAllOf_RedeclarationMarksAPromotedSecretOnly`,
  `schema.TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree`,
  `schema.TestAllOf_ContainerMembersAreJudgedAsFieldsAre`,
  `merge.TestClassify_ComparesResolvedKinds`,
  `merge.TestKindOf_AnEncodedScalarIsReadAsItsValuesTravel`,
  `merge.TestTypeJudge_AnEncodedScalarRelatesAsItsValues`,
  `e2e.TestAllOfFold_AnEncodedFormatIsJudgedAsByteIsBesideBinary`,
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
- Status: partly built: a structure that refuses some kinds only through a `not` or an `if` reads
  as stating its type, so it conflicts with a kind it admits (#1671); two enums of value types neither within the other (`int32` against `uint32`) overlap,
  keeping the later, though they share values, since the IR has no value type for what both admit;
  an alias adding docs to the null it adds is not read as adding null alone, though a reference
  carries an alias's docs to its use, since a use site whose `$ref` chain runs on past the alias
  takes the docs where the chain ends instead (#1029); a model and the wrapper it flattens that fold
  different sets only because the model stops at the flattening depth are refused as
  `compile/registry-refused`, not told apart, since one declaration names both; a member declared at
  two depths keeps the identity of the one written first, so swapping the branches moves it (#1931);
  a hierarchy composing at every level a model redeclaring a name it holds reads its depth again at
  each level, until the run's reads run out (#1993); the names of the models on a cycle of
  compositions decide which declaration they keep where two admit no value together (#1995); a part
  reached through more than 64 alias scalars brings nothing, its member left as the flattening takes
  it, as the type judge, a tag's reading and a multipart body read such a chain no further (#1994);
  and a model the fold composes for a member is minted at the declaration last to widen the field,
  so swapping two branches renames it, and moves a report on a member it settles (#1921, #1992); and
  a type within a variant of a union holding a bound is unrelated to it whatever kind the bound
  applies to, so the later of the two declarations stands, reported (#2005).

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
forbids every key they leave: its value goes, the `additionalProperties` the model took, beside the
`allOf` or on the entry closing it ([`additional-properties`](#additional-properties)), being kept
verbatim under `openapi:additionalProperties` (`degraded_lowering`) at the keyword, and a catch-all
left no pattern goes whole. A `true` conjunct admits everything, so contributing nothing from it is exact and
nothing is reported. Where a `oneOf` or `anyOf` beside the `allOf` is distributed
([`union-beside-structure`](#union-beside-structure)), every composed variant is closed and keeps
the branch, and the branch is reported once.

- Cases: `allof-boolean-branch`, `allof-nested-inline`
- Codes: `openapi/false-schema`
- Tests: `schema.TestAllOf_NestedFalseBranchClosesModel`,
  `schema.TestAllOf_FalseBranchBesideUnionClosesEveryVariant`,
  `schema.TestAllOf_FalseBranchClosesTheCatchAllItForbids`,
  `e2e.TestEntryClosure_AFalseEntryKeepsTheCatchAllAnEntryTypes`

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
admits a set of JSON instance types, an `integer` within a `number` and named as written, and a
number with a fraction, which no `type` names, named a `number that is no integer`, read once
a compile, whatever its size: the `type` beside the `allOf`; an inline entry's `type`, the members
of its `enum` or `const`, the join of what each branch of its `oneOf` or `anyOf` admits, and what
its own `allOf` admits; and for a `$ref` entry, what the schema it lands on admits, read the same
way. The keywords beside a `$ref` entry apply to the instance as the reference does, at 3.0 as from
3.1, since the lowering applies them ([`ref-use-site`](#ref-use-site)), so where they narrow what
it admits they are a conjunct of their own, named `allOf/<n>` beside the reference's
`allOf/<n>/$ref`: `{allOf: [{$ref: Str, type: object}]}` is reported at every version, beside the
later-version warning 3.0 draws. A `oneOf` or `anyOf` beside the `allOf` none of whose branches
shares a kind with the rest of the schema is a conjunct too, named by its keyword and admitting what
any branch does ([`union-beside-structure`](#union-beside-structure)). The schema is reported where those sets share no member, every
conjunct admits some value by itself and none is cut short, and the schema admits no null as
[`nullable`](#nullable) reads it, under the nullable-inference policy where that is on: a null every
conjunct admits is a shared value, as
[`allof-redeclared-property`](#allof-redeclared-property) reads a pair of declarations. A read of
null that runs out of its budget is undecided and reports nothing. Where only one conjunct narrows
the instance types nothing is reported, since one conjunct contradicts nothing, unless it admits no
value by itself and nothing else says so (below).

What writes no `type` and no value set admits every kind, so a report is made only of what the
source declares: a `true` entry, a `format` and a `properties` state nothing. A conjunct admitting no
value by itself, the null included, that says so where it is written is its own to report, and the
`allOf` composing it draws nothing: a `false` entry, which [`allof-false-branch`](#allof-false-branch)
reports, and a `$ref` landing on `false`, on a schema whose value set holds no value its own type
admits, an empty `enum` among them ([`enum-const`](#enum-const)), or on a schema whose own
conjuncts share no value, which is reported where it is written, each read through a chain of
`$ref`s each admitting no value, up to 256 of them. So a schema composing a reported one draws no
second report. Any other conjunct admitting no value is named `no value` in the report, alone or
beside others: an inline entry, which nothing lowers apart from the `allOf`, and a `$ref` landing on
a schema that admits none and says nothing of it, such as a `type` beside a union none of whose
branches it admits, or beside a `$ref` to another kind. A read cut short decides
nothing: a cycle of `$ref`s or `allOf` entries, a `$ref` landing on no schema, a value no reading
converts, or an entry nested past `statement.MaxFlattenDepth`. A `$dynamicRef` the bindings read as
no `$ref` masks nothing, and a `not`, an `if` or a `dependentSchemas` is not followed: a conjunct
may admit less than is read, and conjuncts disjoint as read are disjoint. An `allOf` beside a
`$ref` ([`allof-beside-ref`](#allof-beside-ref)) does not compose and is not read. One passed over
for a `const` or an `enum` ([`competing-keywords`](#competing-keywords)) is read as any composition
is, the value set beside it left to the election, and the warning names the value set the position
lowered as.

- Cases: `allof-disjoint-conjuncts`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestAllOf_DisjointConjunctsReportOnceAtTheSchema`,
  `schema.TestAllOf_DisjointReportStaysSilent`,
  `schema.TestAllOf_AKeywordBesideARefIsReadAtEveryVersion`,
  `schema.TestAllOf_ComposingADisjointSchemaDrawsNoSecondReport`,
  `schema.TestAllOf_DisjointInlineBranchReportsItsResidueAsInfo`,
  `schema.TestAllOf_DisjointConjunctsFollowTheNullableReading`,
  `schema.TestAllOf_DisjointConjunctsIgnoreDeclarationOrder`,
  `schema.TestAllOf_DisjointConjunctsReportOnModelsAndOperations`,
  `schema.TestAllOf_DisjointReportNamesWhatThePositionLoweredAs`,
  `e2e.TestCompile_AUnionConjunctSharingNoKindWithTheOtherIsReported`,
  `e2e.TestCompile_ANullAliasConjunctIsReadAsTheNullItAliases`,
  `e2e.TestCompile_AnEnumAndATypeSharingNoKindAreReportedAtTheSchema`,
  `e2e.TestCompile_AKeywordBesideARefIsReadByTheDisjointReportAtEveryVersion`,
  `e2e.TestCompile_AnEmptyConjunctIsItsOwnSchemasToReport`,
  `e2e.TestCompile_AnEmptyConjunctIsReportedWhereNothingElseSaysSo`,
  `schema.TestToldWhereWritten_ReadsWhatSaysASchemaAdmitsNothing`,
  `e2e.TestCompile_ADisjointReportNamesAFraction`,
  `schema.TestInstanceWords_NamesEverySetOfKinds`

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

Each variant's `Name.Hint` is its branch's: one its title names is the title's
([`naming`](#naming)), and otherwise a `$ref` branch's is its target's name, and an inline branch's
its position (`variant_1`). An inline branch no title names that refines a `$ref`
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
other than `null`, in order, each lowered from the same schema as that type alone, taking only what
applies to it, and named by it: for `object` or `array` a node hoisted at `<pointer>/type/<index>`,
and for a scalar type the shared primitive, or a `Scalar` hoisted at that pointer where the variant
holds what the primitive cannot, a value bound, a format naming an encoding or the content
vocabulary. The pointer holds a type's name and no schema, so nothing else is interned there. A
`null` among them makes the position nullable instead ([`nullable`](#nullable)), so one type and
`null` lower as that type alone, nullable, and a list of `null` alone as a schema of `null` alone
does ([`null-alone`](#null-alone)). The union is exclusive unless two listed types overlap:
`integer` and `number`, the one pair of JSON types that do, since every integer is a number, or a
type listed twice, which [`validation`](#validation) reports. Two variants the types resolve to one
primitive, as a width does ([`numeric-formats`](#numeric-formats)), stay two.

Each variant takes the value bounds its own type takes ([`constraints`](#constraints)), where the
position admits that type, so `[string, integer]` with `maxLength` and `minimum` lowers to a
`string` holding the one and an `integer` holding the other, and the content vocabulary is the
`string` variant's ([`content-vocabulary`](#content-vocabulary)). Beside an `allOf` entry of
`[string, boolean]`, the same list's `integer` variant holds no `minimum`, which is kept on the
union. The property or parameter carrying the union holds no value bound, each being the variant's
it applies to, which a bound on the whole value would not say.
A `format` the table pairs with a listed type is that variant's alone: with `date` the list lowers
to `date | integer`, and with `int32` to `string | int32`. One the table pairs with no listed type
is every scalar variant's encoding name, since nothing says which type it describes and a format
validates only the instances of the types it is defined for (JSON Schema Validation 2020-12 §7.1).
The keyword census asks the variants rather than the union: a keyword or bound some variant holds is
homed, and one none holds, a bound no listed type takes among them, is kept on the union
([`shape-applicator`](#shape-applicator)).

The `object` variant holds no `discriminator`, so the census keeps it on the union, whether or not a
`oneOf` or `anyOf` is written beside it. No `$ref` or `mapping` can name the variant's position,
which holds a type's name and no schema, so no type is its subtype, and every route the
discriminator takes leads elsewhere: a `mapping` target, the `defaultMapping`, or a component whose
`allOf` names the schema, which composes the union and keeps it as its `Base`, with the
`DiscriminatorValue` the discriminator gives it ([`discriminator-value`](#discriminator-value)). Its
values are read as every kept discriminator's are ([`discriminator`](#discriminator)): each position
one names is lowered, and one naming no schema is reported as an `openapi/unresolved-ref` error at
its entry, whether or not the array lists `object`. Settling the variant's discriminator by its
routes, as one beside a union kept verbatim is settled
([`union-beside-structure`](#union-beside-structure)), would keep it only where it routes nowhere,
and would have to find every component composing the schema to know that. Two typed homes lose as
well. A subtype's `Base` naming the variant would compose the schema's object half where its `allOf`
names the schema, and `Base` names what the source wrote
([composition entries](../ir/models.md#composition-entries)). And `pass/validate` holds each route
of a model discriminator to another type to `ir.IsSubtype`, the subtype relation every consumer
reads: a check stepping from a `Union` base into its object variant would part from that relation,
and teaching the relation the step would change what it means for every document.

- Cases: `keywords-at-every-outcome`, `type-array-numeric`, `type-array-discriminator`,
  `type-array-bounds`
- Codes: `openapi/unresolved-ref`
- Tests: `schema.TestLower_MultiTypeUnion`,
  `schema.TestLower_ATypeArrayIsExclusiveOnlyOverDisjointTypes`,
  `schema.TestLower_ATypeListedTwiceIsNotExclusive`,
  `schema.TestLower_ATypeArrayKeepsItsDiscriminatorOnTheUnion`,
  `schema.TestLower_ATypeArrayResolvesItsDiscriminatorRoutes`,
  `schema.TestLower_ATypeArraysBoundLandsOnTheVariantItConstrains`,
  `schema.TestLower_ATypeArraysVariantsSwapWithItsTypes`,
  `schema.TestLower_ATypeArraysFormatIsTheVariantsItPairsWith`,
  `schema.TestLower_ATypeArraysContentIsTheStringVariants`,
  `schema.TestConstraints_ACountAVariantKeepsIsKeptOnce`

### `union-null-variant`

A null branch of a `oneOf` or `anyOf` is one written inline whose own keywords admit `null` alone
([`null-alone`](#null-alone)), whatever spells it: `type: "null"`, an `enum` of `null` alone,
`const: null`, or 3.0's `{type: object, enum: [null], nullable: true}`. It is no variant, of a union
lowered alone or of one distributed across a body beside it
([`union-beside-structure`](#union-beside-structure)), since it would lower on its own to the `Enum`
with no member whose one value the reference's `Nullable` bit states, so the union's reference
admits `null` instead ([`nullable`](#nullable)). A `oneOf` or `anyOf` with exactly one branch that
is not a null branch, beside one or more that are, lowers to a nullable reference to that branch's
lowering rather than to a `Union` ([type-graph](../ir/type-graph.md#type-references)); one with
several lowers to a `Union` of them behind a nullable reference. A `oneOf` of two null branches
admits no `null` by JSON Schema, which `null` would match twice, yet one null branch or several are
read alike, as the union's `null`: the document contradicts itself, and this rule does not read it
otherwise. A `$ref` branch naming a schema of `null` alone stays a variant, a nullable reference to
that schema's `Enum`, which an emitter reads as the union's `null`
([unions](../emitters/refine/unions.md#a-null-a-variant-admits)): it names a node the source
declares, which lifting it would drop, and reading it would take a distant declaration. A branch
whose `null` only the nullable-inference policy admits is a null branch while the policy is on, and
the reference's `null` then names it in `Inferred`, with its `openapi/inferred-nullable` info at the
branch ([`nullable`](#nullable)); with the policy off, the branch admits nothing and is a variant.

A null branch writing a key beside `type`, `enum`, `const`, and a `nullable` its dialect or the
nullable-inference policy reads, such as a `description`, is kept whole under
`openapi:<combinator>/<index>`, the branch's path from the schema, with the reason `no_ir_home`,
since the `null` is typed exactly and only what the branch says of it has no field: on the `Union`
the branches leave, or, where they collapse, where the position keeps what else the collapse cannot
hold, below. It is reported as an info `openapi/degraded-construct` at the branch. A `nullable`
nothing reads is a keyword the branch's dialect does not define, which keeps the branch as any other
does, and a `null` written for one of those keys is the key left out
([`null-keywords`](#null-keywords)), and keeps nothing. An `enum` that repeats its `null` is the
document's whichever path takes the branch, so each repeat is reported as an
`openapi/duplicate-enum-value` warning at it, as for that `enum` written alone
([`enum-const`](#enum-const)).

The collapse is held to a `$ref` site's rule ([models](../ir/models.md#composition-entries)), since
it too lowers to a reference: a collection bound beside it reaches what the position owns
([`constraints`](#constraints)), and a keyword beside it that no reference holds — a
`discriminator`, a `format` other than `password` — is kept verbatim under `openapi:<keyword>`
(`degraded_lowering`) on the node or carrier owning the position, with an info
`openapi/degraded-construct` at the position. A null branch kept whole lands beside them: on the
carrier of a property, parameter or header, and on an alias hoisted at any other position.

- Cases: `nullable-union-discriminator`, `nullable-31-ref`, `null-alone`, `enum-null-member`
- Codes: `openapi/degraded-construct`, `openapi/inferred-nullable`, `openapi/duplicate-enum-value`
- Tests: `schema.TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold`,
  `schema.TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition`,
  `schema.TestNullAlone_ReadsTheSchemasOwnKeywords`,
  `schema.TestNullBranch_IsTheReferencesInEverySpelling`,
  `schema.TestNullBranch_EverySpellingLowersAlike`,
  `schema.TestNullBranch_ARefToANullAloneSchemaStaysAVariant`,
  `schema.TestNullBranch_ADiscriminatorLinesItsVariantsUpPastTheNull`,
  `schema.TestNullBranch_KeepsWhatTheBranchWrites`,
  `schema.TestNullBranch_AKeptBranchTheKeeperCannotConvertIsReportedByIt`,
  `schema.TestNullBranch_KeepsANullableNothingReads`,
  `schema.TestNullBranch_ARepeatedNullIsReportedAtTheBranch`,
  `schema.TestNullableInference_EachShapeReadsByItsVersion`

### `union-null-only`

A `oneOf` or `anyOf` whose every branch is a null branch
([`union-null-variant`](#union-null-variant)) admits what a schema of `null` alone admits, so it
lowers as one does at that position, to an `Enum` with no member behind a nullable reference
([`null-alone`](#null-alone)). The `Enum` carries no branch set, so the combinator, and the other
one too where both are written, is kept verbatim under `openapi:oneOf` or `openapi:anyOf`
(`degraded_lowering`), with an info `openapi/degraded-construct` at the schema, and what else the
schema writes that the `Enum` has no field for is kept by the census, as beside a schema of `null`
alone.

- Cases: `null-only-union`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestOneOfAnyOf_NullOnlyLowersAsNullAlone`,
  `schema.TestLowerNullAlone_KeepsWhatTheEnumCannotHold`

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
finds too. The referent is the variant's `Base` or a `Mixin` only where the branch lowers to an
object alone, null aside, or its read is cut short, as an `allOf`'s `$ref` entry composes
([`allof-classification`](#allof-classification)), and a `$ref` to a schema whose `type` list holds
an object composes that list's object variant, unless the branch writes a keyword beside its `$ref`,
which its own node holds over the whole list. Any other branch, admitting another kind or every
kind, is kept verbatim on its variant under its path from the schema, `openapi:oneOf/<i>` or
`openapi:anyOf/<i>` (`degraded_lowering`), with an info `openapi/degraded-construct` at the branch,
and still lowers at its own pointer ([models](../ir/models.md#composition-entries)). A variant holds
the bounds written beside the union that the kinds it admits take, its branch's within the body's,
and one no variant takes is kept on the `Union` ([`constraints`](#constraints)): beside `type:
object`, a variant over a string holds no property count. Object keywords written with no `type`, a
`properties`, a `patternProperties`, a `required`, a closure or a catch-all, are a body too where
the kinds the position lowers to are an object alone ([`shape-applicator`](#shape-applicator)):
what the branches declare, each an object, or, where nothing declares a kind, what the
kind-inference policy reads off them. Each variant, a model over its branch, then holds those
keywords, as beside `type: object`: `{oneOf: [{$ref: A}, {$ref: B}], additionalProperties:
false}` is a `Union` of closed models over `A` and `B`, and a bare `required` names the members each
variant requires. A null branch is no variant of a distributed union, as it is none of a plain one
([`union-null-variant`](#union-null-variant)): the reference to the position admits `null` where
the body admits it too, so beside a body writing `type: object` it admits none, and beside one
writing no `type`, or listing `null` in its `type`, it admits the union's
([`nullable`](#nullable)), and a union written on an `allOf` branch distributes past its null branch
alike.

A `discriminator` standing with the union, where no branch composes the schema (shape 3), routes
the distributed union ([`discriminator`](#discriminator)): a `mapping` target, or the
`defaultMapping`, naming the component a `$ref` branch names routes to the variant composed over
that branch. OpenAPI routes a variant no entry names by the name of the component its branch
names, which a composed variant, named for its position, does not carry, so that route is written
into the `Mapping`, as [`discriminator`](#discriminator) writes it wherever a nil one would not say
it. No variant carries a `DiscriminatorValue`, which tags a subtype in a hierarchy, since the
mapping's keys tag the union's variants, and
[`discriminator-inference`](#discriminator-inference) may read the mapping off the tags they pin.

A `oneOf` or `anyOf` written as an inline branch of the schema's `allOf` conjoins with the rest of
the composition as one written beside the `allOf` does, so it lowers the same way, where the schema
writes no union of its own, its `allOf` composes rather than refines
([`allof-refinement`](#allof-refinement)), and the union is the one branch union whose branches
declare shape (below), or of several the one written as a `oneOf`, as
[`competing-keywords`](#competing-keywords) elects the `oneOf` of one schema writing both: each other
is kept on every variant, whichever is written first. The `discriminator` standing with it is the one its branch writes, else the
one beside the `allOf`: where both write one, nothing says which routes the union, so it stays whole
with its branch and the `Model` holds the schema's own. It routes the union as one beside it does,
a `mapping` entry naming no schema reported at the entry under the schema writing it, and it routes
by name the schemas the union lists ([`discriminator-value`](#discriminator-value)). Its variants
take their IDs from the branch's pointers, its `$ref` and inline branches conjoin as above, and the
branch keeps nothing of the union or of a `discriminator` it writes, whose extensions and keys
OpenAPI defines for no Discriminator Object ride on the `Union` holding it
([`vendor-extensions`](#vendor-extensions)), what else the branch writes merging as any branch does
([`allof-inline-residue`](#allof-inline-residue)). Whether a route names the schema
is read at the position, as for a union beside the `allOf`, so an inline schema below a
discriminated base no route names distributes. A union on a branch that is one of the shapes below,
one of two branch unions declaring shape and written under one keyword, distributing either of which
would drop the other and no rule ranking them, or one on a branch nested past the flattening depth
stays whole with its branch, and so does one whose branches declare no shape, on each variant where
another union distributes. Distribution cannot
reach these shapes:

1. the structural body is not a `Model`: a `const`, an `enum`, a scalar, or keywords written with
   no `type` where the kinds the position lowers to are not an object alone, beside a branch that
   may be no object, or where a cycle cuts the read short and the keywords infer no object, so there
   are no composition fields for the variants to carry;
2. a branch is a `$ref` that names no referent the compilation resolves — a cross-document target
   it does not intern, an undeclared component, a pointer addressing no schema — so it declines
   rather than composing a variant whose branch quietly went missing;
3. the schema declares a `discriminator`, the one standing with the union, and a branch composes
   the schema through `allOf`, at any depth (`Cat: {allOf: [{$ref: Pet}, …]}`): the schema is the
   base of a hierarchy the discriminator routes, which a union of composed variants would no longer
   be the base of;
4. `oneOf` and `anyOf` are both declared, so distributing one would drop the other;
5. no branch of a union beside an `allOf` shares a kind with what the rest of the schema admits, so
   no instance satisfies the two together and a variant would hold no value: the position lowers as
   the rest does ([`allof-refinement`](#allof-refinement)), and the schema draws the warning of
   [`allof-disjoint-conjuncts`](#allof-disjoint-conjuncts) naming the union.

In each, whatever structural body the schema declares is lowered — `properties` or a declared
`type` yields a `Model` or typed node — and the union branches are kept verbatim in
`Unmodeled["openapi:oneOf"]` / `["openapi:anyOf"]` (`degraded_lowering`), reported as an info
`openapi/degraded-construct` at the schema naming which applied; in case 2 the broken
reference is reported as well, as an `openapi/unresolved-ref` error, or, on a loop the compiler
reads, by the loop's one `openapi/cyclic-ref` ([`reference-cycles`](#reference-cycles)). Case 1's
body-less sibling leaves nothing to build: it reduces to `any`, and the sibling itself is kept by
[`shape-applicator`](#shape-applicator) rather than dropped, so the union's diagnostic and the
sibling's are both reported. A `null` branch in this shape is **not** lifted to `Nullable`: the
union it belongs to is no longer structural, so lifting one branch out of it would assert something
the lowered type does not say. The degradation is that the generated SDK is *under-constrained* — it
admits values the union would reject — which is why the branches must survive for a validating
emitter to reach.

A schema a `discriminator` on an `allOf` ancestor routes to, by a `mapping` key or by its name as a
component ([`discriminator-value`](#discriminator-value)), is not distributed either: the route names
what the schema lowers to, which as a `Union` would be no subtype of that ancestor (`ir.IsSubtype`,
what `pass/validate` holds a route to another type to). It stays the `Model` its composition lowers
to, under that ancestor and with the tag that rule gives it, and composes its union by reference, as
an `allOf` naming a component holding the union does
([models](../ir/models.md#a-union-a-composition-names)):
its last `Mixin` names a `Union` of the union's branches, each lowered as a union written alone
lowers it. No schema occupies that `Union`, so it is minted at the keyword writing the union,
`…/Cat/oneOf` beside the `allOf` or `…/Dog/allOf/1/oneOf` on a branch, in the `composed`
namespace, apart from the branch's own node, which an outside `$ref` to the branch still finds. The
`Model` keeps nothing of the union, what a branch writes beside it merging as any branch does, and a
`discriminator` standing with the union is the `Union`'s, routing its variants, so the `Model`
holds none, but for one naming the empty string as its tag, which the `Union` cannot hold and the
`Model` lowers and keeps as [`discriminator`](#discriminator) says. It is reported as an info `openapi/composition-lowering` at the schema. Whether a route
names the schema is read at its position, and a discriminator written beside a union routes by name
only what that union lists, so beside a schema it does not list, and no `mapping` key names, a union
distributes.

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
keeps the body where a branch composes it (shape 3).

A `discriminator` beside any of these shapes whose body lowers to a `Model`, shape 3 written on an
`allOf` branch among them, stays on that `Model` only when every type it routes to is a subtype of it
at any depth (`ir.IsSubtype`): each `mapping` target,
the `defaultMapping`, and each component a `$ref` branch names, which it routes to by name —
`oneOf`'s branches where `oneOf` and `anyOf` are both declared, since `oneOf` wins. An inline branch,
or one naming no component, is no route unless the `mapping` names it, and a route to the `Model`
itself is none of its subtypes, though `pass/validate` takes one where the model is concrete.
Otherwise the whole object (`propertyName`, `mapping`, `defaultMapping`) is kept verbatim in
`Unmodeled["openapi:discriminator"]` on the node keeping the union, with an info
`openapi/degraded-construct` at the discriminator; a subtype keeps the `DiscriminatorValue` its
mapping key gives it, since that is still the tag its instances carry, and a body the move leaves
holding a map and nothing else lowers to that map's `MapT` ([containers](../ir/containers.md)). A
union whose branches declare no shape (`validation_only`) is held to the same rule. A multi-typed
body leaves no discriminator to settle: [`type-arrays`](#type-arrays) keeps it on the `Union` the
body lowers to, beside the kept union. The compiler decides once every node is interned
(`compile.Settlement`), because neither the walk nor the source schemas can answer it: a subtype
declared after the `Model` has no `Base` while the `Model` lowers, and a schema whose `allOf` names
the `Model` may still lower to no subtype — an `enum`, a `const`, or an alias over a `$ref` beside
it.

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
a branch writes counting its members, and the body only as far as they need. The body is read
twice, each reading within bounds of its own: first what the schema itself writes, its own keywords
and its own `properties` and `items`, nothing it composes, which judges every branch; then, for each
branch that reading does not find restating, the whole body as points 2 and 3 read it. A branch
restating the first restates the second, more declarations admitting less, so the first settles
what the schema's own keywords settle however long its composition, and where it does not finish it
settles none, so which branches it settles never turns on their order. Within a reading, a branch
writing only keywords point 1 names reads none of it, the declarations of what a schema nests are
read only where it nests something, and what the body declares of one property or element is read
once, however many branches restate it. A read that does not finish says nothing, so its union is
judged as one whose branches declare shape.

- Cases: `allof-oneof-cooccurrence`, `discriminated-union-beside-model`,
  `discriminated-union-distributed`, `unhomed-keywords`,
  `keywords-at-every-outcome`, `union-restated-branches`, `union-restated-type`,
  `union-inline-branches`, `allof-union-branch`
- Codes: `openapi/composition-lowering`, `openapi/degraded-construct`, `openapi/unresolved-ref`,
  `openapi/validation-only-keyword`
- Tests: `schema.TestOneOf_CoDeclaredNotDistributedReasons`, `schema.TestLower_AllOfWithOneOfKeepsBoth`,
  `e2e.TestAllOfUnion_AUnionSharingNoKindWithTheRestIsKeptNotDistributed`,
  `schema.TestOneOf_CoDeclaredKeywordsEveryBranchSaysLeaveTheUnionAlone`,
  `schema.TestOneOf_CoDeclaredTypeReadsNullableByItsDialect`,
  `schema.TestOneOf_CoDeclaredInlineBranchIsConjoinedByItsNode`,
  `schema.TestOneOf_CoDeclaredNonModelBranchIsKeptOnItsVariant`,
  `schema.TestOneOf_CoDeclaredUnpreservableBranchIsNotAnnounced`,
  `schema.TestOneOf_CoDeclaredBranchReadCutShortIsComposed`,
  `e2e.TestVariantBounds_AVariantNamesOnlyAnObjectBranchAsItsBase`,
  `e2e.TestVariantBounds_AVariantOverATypeListComposesItsObject`,
  `e2e.TestVariantBounds_AVariantKeepsATypeListOrAUnionHoldingNoLoneObject`,
  `e2e.TestVariantBounds_ATypeListBranchWritingBesideItsRefIsKept`,
  `schema.TestOneOf_CoDeclaredTypeListBranchComposesItsObject`,
  `schema.TestOneOf_CoDeclaredBareRequiredConjoinsObjectBranches`,
  `schema.TestUnion_AListOfItemsBesideTheBranchesIsNotSaid`,
  `schema.TestAllOf_AUnionBranchDistributesAsBesideTheAllOf`,
  `e2e.TestElection_TwoUnionsBesideABodyDistributeTheOneOf`,
  `e2e.TestElection_TwoAnyOfUnionsBesideABodyDistributeNeither`,
  `e2e.TestElection_TwoOneOfUnionsBesideABodyDistributeNeither`,
  `schema.TestUnion_ARoutedSubtypeComposesItsUnionAsAMixin`,
  `schema.TestUnion_ARoutedSubtypeSharedByTwoOperationsComposesItsUnionOnce`,
  `schema.TestComposeMixin_NeedsAModel`, `schema.TestUnion_ABranchUnionNoRouteNamesDistributesAsBeside`,
  `schema.TestAllOf_ADiscriminatedBranchUnionDistributesAsBeside`,
  `schema.TestAllOf_ADiscriminatedBranchUnionReportsItsMappingAtTheBranch`,
  `schema.TestAllOf_ABranchDiscriminatorKeepsWhatNoFieldHolds`,
  `schema.TestAllOf_AHierarchyBaseBranchUnionHoldsItsDiscriminatorToItsRoutes`,
  `schema.TestOneOf_CoDeclaredBesideAComposingAllOfDistributes`,
  `schema.TestOneOf_CoDeclaredRestatementOfTheBodyItselfOutlastsItsComposition`,
  `schema.TestOneOf_CoDeclaredOwnReadingSettlesInAnyBranchOrder`,
  `schema.TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed`,
  `schema.TestOneOf_CoDeclaredBranchAfterASkippedOneIsReported`,
  `e2e.TestCompile_ABranchOnALoopIsReportedOnlyAsTheLoop`,
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
  `schema.TestUnion_ASchemaNoRouteNamesDistributesBesideItsAllOf`,
  `schema.TestNullBranch_BesideABodyIsLiftedPastTheDistribution`,
  `e2e.TestUnionNullVariant_ADistributedUnionLiftsItsNullBranch`,
  `e2e.TestUnionNullVariant_AUnionOnAnAllOfBranchLiftsItsNullBranch`
- Status: partly built: a discriminator routing to the model holding it moves though that route is
  legal (#1806); a union beside a `$ref` sharing no kind with its target is kept by
  [`union-beside-ref`](#union-beside-ref) rather than as shape 5 (#2010).

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
declaration's keywords. The same preference decides between a `oneOf` and an `anyOf` written on two
entries of one `allOf`, whether it refines ([`allof-refinement`](#allof-refinement)) or composes
([`union-beside-structure`](#union-beside-structure)), whichever is written first, the `anyOf` kept
with its entry. Where a *structural* sibling is written as well,
[`union-beside-structure`](#union-beside-structure) applies instead and both branch sets are kept,
an `allOf` that refines being no such sibling but a conjunction the unions are conjuncts of
([`allof-refinement`](#allof-refinement)).
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
  `schema.TestUnionCombinators_UnpreservableIsNotAnnounced`,
  `e2e.TestElection_BothKeywordsElectTheOneOfWhicheverIsWrittenFirst`,
  `e2e.TestElection_TwoUnionsBesideABodyDistributeTheOneOf`

### `discriminator`

A `discriminator` on a schema that lowers to a `Model` or a `Union` lowers to its `Discriminator`,
and so does one standing with a union on a branch of a schema's `allOf`, written on that branch or
beside the `allOf`, which the `Union` the union lowers to holds
([`union-beside-structure`](#union-beside-structure)).
On a `Model` whose own properties include the one `propertyName` names, the tag is that property's
`PropID`, in `Discriminator.Property`; otherwise, and on every `Union`, it is the name as written,
in `Discriminator.PropertyName`. A `propertyName` that is the empty string, a legal member name, has
no spelling there, since an empty `PropertyName` means none, so where no own property of a model
carries it the node holds no `Discriminator`, and the census keeps the whole object verbatim and
says why ([`shape-applicator`](#shape-applicator)): a union so written is untagged, and
[`discriminator-inference`](#discriminator-inference) reads nothing for it. Each `mapping` entry
lowers to its key and the type its value routes to, and 3.2's `defaultMapping` to
`Discriminator.Default`. `Inferred` is not set: the document declares the discriminator. OpenAPI
reads a tag per value, as the name of a component schema unless a `mapping` entry keys that value,
its implicit mapping: 3.0.4, 3.1.1 and 3.2.0 say so ("_unless_ a `mapping` is present for that
value"), and 3.0.3 and 3.1.0, which those patch releases clarify, fail a value only where it matches
no "implicit or explicit mapping". Their example maps the value `dog` to `Dog` "rather than the
default (implicit) value of `Dog`", which the patch releases correct to "of
`#/components/schemas/dog`", the reading `dog` itself takes with no entry. So the name of the
component a variant's `$ref` branch names routes to the variant whether or not an entry routes it
too. The IR routes by a variant type's name only where a union's `Mapping` is nil
([models](../ir/models.md#discriminator)), so a union's `Mapping` holds the name route to each
variant no entry routes, the name keying its variant, wherever a nil one would not say it: where the
`mapping` holds an entry, or where a variant's type is not the component its branch names, as an
alias for a keyword beside the `$ref` is not, nor a variant composed over its branch
([`union-beside-structure`](#union-beside-structure)). A name two branches name, or one an entry
spells already, keys nothing more, and an inline variant has none. The compiler leaves out the name
of a variant an entry routes, though OpenAPI routes by it: a `Mapping` is what a consumer writing an
instance picks the variant's tag from, and a map cannot mark one of two routes to a variant as read
and never written, so the name written in would give the variant a second tag to write and nothing
to say which. A hierarchy leaves out the name of a subtype a key tags in the same way, its
`DiscriminatorValue` being a key ([`discriminator-value`](#discriminator-value)). A payload
carrying such a name routes nowhere in the IR, or to `Default` where a 3.2 document declares
`defaultMapping`. A variant the `defaultMapping` names and no entry routes is still routed by its
name. With no `mapping`, and each variant whose branch names a component that component itself, the
`Mapping` is nil and a union routes each variant by the name of its type, unless
[`discriminator-inference`](#discriminator-inference) reads it off the tags the variants pin. A
subtype the mapping names under several keys — an alias tag — takes the smallest in byte order as
its `DiscriminatorValue` ([`discriminator-value`](#discriminator-value)), since the field holds one
and a mapping's key order is not a property of the document, with every key kept in the base's
`Mapping` and the election reported as an info `openapi/degraded-construct` at the subtype. A
discriminator beside a node that routes by none is kept verbatim by
[`shape-applicator`](#shape-applicator), and one beside a union written with structure, or on an
`allOf` branch writing a union, is [`union-beside-structure`](#union-beside-structure)'s.

A `mapping` value, and a `defaultMapping`, is read as OpenAPI 3.0.4, 3.1.1 and 3.2.0 read it: "The
`mapping` entry maps a specific property value to either a different schema component name, or to a
schema identified by a URI", and the behaviour of a value "that is both a valid schema name and a
valid relative URI reference is implementation-defined, but it is RECOMMENDED that it be treated as a
schema name". So a value that is the key of a schema component the entry document declares names
that component, whatever its characters and in whichever document the discriminator is written, as
OpenAPI recommends resolving a component's name "from the entry document, rather than the current
document". Any other value is a URI reference written at the discriminator, with a fragment or
without (`https://example.com/cat`, `./cat.yaml`, `cat.yaml`, `other.yaml#/Cat`,
`#/components/schemas/Cat`), read as a `$ref` written there is ([`references`](#references)). A value
that is neither is reported as an `openapi/unresolved-ref` error at its entry, which the `Mapping`
leaves out, and one holding no `#` says both readings failed. Reading every value as a reference
first would read a file named `Dog` beside each document writing `dog: Dog`, and reading a value
shaped like a component's name only as a name would refuse `cat.yaml`, which 3.0.3 and 3.1.0, whose
text asks for no `./` prefix on a relative reference, let a document write.

From the position a value names, its chain follows each `$ref` in turn, a component's as much as any
other position's, to the first position writing none, at most 64 `$ref`s, a longer chain stopping
where the bound reached. A position writing a `$ref` lowers to an alias over what the `$ref` names,
whatever is written beside it, and an alias is no subtype and no variant, so a route means such a
position only where a union's variant is that alias. On a `Model` a value routes to the type
declared where its chain ends, and a subtype's tag is read off the same chains
([`discriminator-value`](#discriminator-value)). A union's `Mapping` names variants, which is what
`pass/discriminator-missing-variant` holds it to and what an emitter routes by, so on a `Union` the
first position of the chain a branch meets decides: the variant whose branch is that position or
whose type it is, and otherwise the variant whose branch's own chain of `$ref`s passes it, the first
in branch order where several do. Several branches each aliasing one type for a keyword beside its
`$ref` are told apart by nothing the value says, and the route must name a variant, so it names the
first, reported as an info `openapi/degraded-construct` at the entry naming the branches; neither
the mapping's key order nor any lowering order decides. A chain no branch meets routes to the type
it ends on, as on a model, which `pass/discriminator-missing-variant` reports as no variant, the
compiler reporting nothing. Where a position on a chain writes a `$ref` landing on no schema, or on
a loop, the value names no type: its entry is left out, unreported, since the failing `$ref` is
reported where it is written, and the last position the chain reached is lowered all the same. A
union's value naming the failing branch itself still routes to that branch's variant, as the branch
meets the chain before it breaks. A discriminator the census keeps verbatim has its values read in
the same way, what each chain ends on lowered and a value naming no schema reported, since a value
is a reference wherever its discriminator ends up.

- Cases: `oneof-discriminated`, `discriminator-inheritance`, `discriminator-default-mapping`,
  `discriminator-alias-mapping`, `nullable-union-discriminator`, `discriminator-aliased-branch`
- Codes: `openapi/degraded-construct`, `openapi/unresolved-ref`
- Tests: `schema.TestHomelessDiscriminator_IsKeptBesideEveryOtherNode`,
  `schema.TestUnplacedDiscriminator_IsKeptAtEveryUnionSite`,
  `schema.TestKeptDiscriminator_ResolvesItsValuesAtEverySite`,
  `schema.TestDiscriminator_AnEmptyPropertyNameIsKeptVerbatim`,
  `schema.TestDiscriminator_AnEmptyPropertyNameBesideAnUntaggedUnionIsKeptForItsNode`,
  `e2e.TestMappingTargets_ADiscriminatorAReferenceReachesIsBoundWhereItSits`,
  `bind.TestBind_ReadsAMappingValueNamingNoComponentAsAReference`,
  `load.TestBindSchemas_ReadsTheComponentNamesTheModelReads`,
  `schema.TestDiscriminatorMapping_ReadsAValueNamingNoComponentAsAReference`,
  `schema.TestDiscriminatorMapping_ReadsAComponentsNameFirst`,
  `schema.TestDiscriminatorMapping_ReportsAValueNamingNothingOnce`,
  `schema.TestMappingWhy_SaysBothReadingsOfAValueWithNoHash`,
  `schema.TestDiscriminatorMapping_ReadsThroughAComponentThatIsARef`,
  `schema.TestDiscriminatorMapping_ChainOfPositionsDeeperThanCap`,
  `schema.TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget`,
  `schema.TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget`,
  `schema.TestDiscriminatorMapping_RoutesToTheNearestVariant`,
  `schema.TestDiscriminatorMapping_ATypeSeveralVariantsAliasRoutesToTheFirst`,
  `schema.TestDiscriminatorMapping_LeavesAValueNamingNoVariantToValidate`,
  `schema.TestDiscriminatorMapping_ABrokenChainDrawsOnlyItsRefsError`,
  `schema.TestDiscriminatorMapping_WritesTheRoutesItImplies`,
  `schema.TestMappingTagsFor_ReadsTheMappingOnce`
- Status: partly built: a variant whose branch names a component of another document is routed by
  its type's name where the `Mapping` is nil, and by nothing where it holds an entry (#1829); and a
  payload tagged with the name of a union's variant or a hierarchy's subtype that an entry routes
  reaches no variant, or `Default`, though OpenAPI routes it by that name, as the IR has no place
  for a route a consumer reads and never writes (#1863).

### `discriminator-value`

A subtype's wire tag lowers to `Model.DiscriminatorValue`, left empty on the base itself. It is read
off the discriminators of the subtype's `allOf` ancestors — the `$ref` branches' targets, then
theirs, level by level to the walk's depth bound, nearest first, a schema that declares none passed
through and no schema its own ancestor — as the key an ancestor's `mapping` spells for the subtype,
the mapping naming it by reference or by schema name, read through each `$ref`, a component's among
them, as [`discriminator`](#discriminator) reads it, an inline subtype by its position's
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
on one of its branches, since a union would be no subtype: it composes the union as a mixin
([`union-beside-structure`](#union-beside-structure)), so no variant of a distributed union carries
a tag. A discriminator standing with a union on a branch of its schema's `allOf`, written on that
branch or beside the `allOf`, routes by name the schemas that union lists, as one beside it does.

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
which `false` turns off (invariant 6). What an instance admits is read off the IR, each type read
through alias scalars. A value of a type admits a string `Literal`'s one string, the members of a
closed `Enum` every one of which is a string, or what a value of a closed union does: the strings
any of its variants admits, a `oneOf`'s as an `anyOf`'s. A value two variants of a `oneOf` admit
matches neither, but what is read of a variant can admit more than it holds, as a constraint kept
beside an `Enum` rules some of its members out, so a string read off two variants may still match
one alone. A union on a cycle of such unions, or reaching one, admits any value, and any other type,
a number among them, any string, since a discriminator's tag is the wire string, which a number's
text spells too. A value admits null where its reference or an alias it is read through does, or one
of a union's variants does, and null is no tag. A member admits what every declaration its
flattening reads of it admits (`ir.FlattenedProperties`), the one it takes and those it overrides,
as an instance meets them all, and null only where each does. An object admits at a property what
its member of that name admits, any value where it declares none, met with what each closed union it
composes admits there: what any of the union's variants admits as an object in turn, a variant that
is no model or union admitting any value. A cycle of objects and the unions they compose, as a union
listing subtypes that compose it, is read alike from wherever the reading enters it, each member
admitting what it does given what the others admit, from any value down until none changes: so a
subtype pinning its tag admits its pin, and the union what its subtypes admit. A cycle is swept at
most 64 times, each sweep reading every member off the one before; one still changing then is read
as the last sweep left it, which admits no less than its instances hold, so nothing is reported.

So a body beside a union that redeclares the tag as a string, which its composed variants take,
leaves each branch's pin in force ([`union-beside-structure`](#union-beside-structure)), and a tag
written as a `oneOf` of `const`s reads as one written as an `enum`, the set
[`string-const-union`](#string-const-union) later normalizes it to. A variant pins a property where
it admits one or more strings and nothing else. The reading is a settlement (`compile.Settlement`):
it waits for every node to be interned, since a variant lowered after its union has no node while
the union lowers, and for every redeclared field to fold
([`allof-redeclared-property`](#allof-redeclared-property)). No settlement after it changes what it
finds, whichever settles first, and each type is read once a run, however many unions and subtypes
ask. Two unions and a subtype meet it:

1. A `discriminator` declaring no `mapping` routes each variant by the name of the component its
   `$ref` branch names, OpenAPI's implicit mapping, which reaches no inline variant, names a tag a
   variant pinning other strings rejects, and routes no other string a variant admits. Where every
   variant pins the `propertyName` to strings no other variant admits, and some variant admits other
   than its name alone, the `Mapping` maps each string to the variant admitting it, the union's
   `Provenance.Inferred` names `discriminator-inference`, and an info
   `openapi/inferred-discriminator` at the discriminator names the first variants it reads
   otherwise, with the first strings each admits. `Discriminator.Inferred` stays false: the tag
   property is declared. Variants sharing a string leave the routes to the document, since a
   `Mapping` routes a tag to one variant.
2. A closed union of two or more variants declaring no discriminator, no variant admitting null,
   gets one where every variant requires a property and pins it to strings no other variant admits:
   the first such property in the first variant's layout order is its `PropertyName`, each string
   maps to the variant admitting it, and the union is `WireTagged`, `Discriminator.Inferred` set
   and `Provenance.Inferred` naming `discriminator-inference`. Each variant requiring a value no
   other admits, a payload names its variant as validation finds it, departing from nothing the
   document says, so nothing is reported. An open union may meet a variant carrying any tag
   ([unions](../ir/unions.md#open-unions)), and is left alone.
3. A subtype a discriminator routes by its name ([`discriminator-value`](#discriminator-value))
   carries that name, which validation rejects where the subtype admits only other strings for the
   `propertyName`, read as an object, null being no tag. Where that rules the name out, the subtype
   carries the one string it admits, or, admitting several or none, no tag, since none routes to it;
   its `Provenance.Inferred` names `discriminator-inference`, and an info
   `openapi/inferred-discriminator` at it names what it admits. Each subtype is read alone, so two
   admitting one string both carry it, as their instances do
   ([models](../ir/models.md#discriminator)).

- Cases: `discriminator-inferred`
- Codes: `openapi/inferred-discriminator`
- Tests: `schema.TestDiscriminatorInference_MapsAnUnmappedDiscriminator`,
  `schema.TestDiscriminatorInference_LeavesADeclaredMapping`,
  `schema.TestDiscriminatorInference_NamesAFewDisagreements`,
  `schema.TestDiscriminatorInference_InfersAnUndeclaredDiscriminator`,
  `schema.TestDiscriminatorInference_LeavesALoneOrNullableVariant`,
  `schema.TestDiscriminatorInference_DisabledReadsNothing`,
  `schema.TestDiscriminatorInference_ReadsADistributedUnion`,
  `schema.TestUnionTags_ReadsOnlyWhatItWasDeferredFor`,
  `schema.TestTagReadings_ValueReadsAClosedSetOfStrings`,
  `schema.TestTagReadings_AUnionOnACycleAdmitsAnything`,
  `schema.TestTagReadings_MemberMeetsEveryDeclaration`,
  `schema.TestTagReadings_ObjectMeetsWhatAComposedUnionAdmits`,
  `schema.TestTagReadings_ACycleOfCompositionReadsAlikeFromAnyEntry`,
  `schema.TestTagReadings_SweepsACycleAtMostTheBound`,
  `schema.TestTagReadings_SweepsACycleUntilItsNullsSettle`,
  `schema.TestTagReadings_ReadsASharedTypeOnce`, `schema.TestThroughAliases_ReadsAtMostTheBound`,
  `schema.TestTagReadings_ReadAModelAsTheMapItSwapsFor`,
  `schema.TestModelFields_ReportsAFoldFollowingAStoredReading`,
  `schema.TestDiscriminatorInference_ReadsTheTagASubtypeAdmits`,
  `schema.TestDiscriminatorInference_ReadsAUnionItsSubtypesCompose`,
  `schema.TestSubtypeTagReading_ReportsWhatItCannotSettle`,
  `schema.TestDiscriminatorInference_JoinsTheMarksANodeCarries`
- Status: partly built: a `mapping` naming some variants is read as written, so a route it implies
  by a name its variant rejects stands (#1798).

### `string-const-union`

A `oneOf` or `anyOf` whose every branch is a string `const`, a string `enum` or such a union is a
closed set of strings in another spelling. The compiler lowers it as any union ([`union`](#union)),
each branch to its `Literal` or `Enum` holding what the branch says of its values, and the engine's
`const-unions` pass, not the compiler, normalizes it to the closed `Enum` of those values at the
union's own ID, so an emitter meets one shape however the document wrote the set. A branch that is
such a union is read as the values it holds, in the same run, so no second run finds one. Each
member is identified and named as the same value in an `enum` is ([`enum-const`](#enum-const)), in
branch order, and holds what is said of its value: a branch of one value gives its member the
branch's docs, deprecation, availability, examples and kept constructs, and a branch of several
gives each of its values what that value's own member holds. A branch's `title` is its member's
`Docs.Summary` and no name of it: the member is named by its value, as an `enum`'s is, so the two
spellings of one set name its members alike, and a branch's type nothing else references, whose
hint the title is ([`naming`](#naming)), goes with the union.

The union stays where an `Enum` could not say all it does: a branch naming a component, a type of
its own that an `Enum` member is not; a branch holding several values that another node names too,
or a member of which another node names, a set of its own that the `Enum` would copy into each
union naming it, which a document can make grow with the square of its size; a branch holding
several values that it or its variant says anything of, which no one member could hold; a value two
branches hold, which a `oneOf` rejects and whose two branches may say two things of it; values typed
by two primitives; a nullable branch; a construct a branch keeps verbatim for any reason but being a
vendor extension, such as a constraint its `Literal` has no home for, which narrows the branch and
would not narrow a member; a branch whose `Provenance.Inferred` names a heuristic, whose mark no
member could carry; one fact two places say of one value; and a bound written beside the union,
which its `Constraints` hold ([`constraints`](#constraints)) and an `Enum` has no field for. A union
of numbers, or of values of several kinds, is outside this rule and stays a union.

- Cases: `string-const-union`
- Tests: `e2e.TestStringConstUnion_NormalizesToItsEnumSpelling`,
  `e2e.TestStringConstUnion_ABoundBesideTheSetKeepsTheUnion`

### `enum-const`

An `enum` lowers to a closed `Enum` at any member count, one member included, each entry a member
holding its value ([`values`](#values)) in source order ([enums](../ir/enums.md)). A `null` entry is
no member and no variant: whether the position admits null is its reference's
([`nullable`](#nullable)), as for a `oneOf` holding a `type: "null"` branch, so the fact is stated
once, and an `enum` listing `null` alone lowers to an `Enum` with no member, which a nullable
reference makes a position admitting null and nothing else. A keyword beside the `enum` that
excludes null, as `type: string` does, leaves a `null` entry admitting no instance, since JSON
Schema conjoins the two, so it is dropped and reported as an `openapi/degraded-construct` warning at
it. Its `ValueType` is the primitive one declared `type` and its `format` select, as a scalar's are
read ([`string-formats`](#string-formats), [`numeric-formats`](#numeric-formats)), else the
primitive of the `type` alone, where every member is a value of that primitive, else the primitive
of the members. A `format` that reaches no `ValueType`, whether the table lacks the pairing, as it
lacks each whose members are a text the value travels as, `byte` and `http-date` among them, or a
member does not fit it, as none is a value of `binary`'s `bytes`, is kept verbatim under
`openapi:format` (`degraded_lowering`), with an info `openapi/degraded-construct` at the schema, but
for `password`, which is a redaction ([`password`](#password)). A member that no declared `type`
admits, as `"a"` under `type: integer` or under `type: "null"`, matches no instance either, and is
reported as an `openapi/degraded-construct` warning at it; a `!!binary` member is the string of its
text to the `type`, as the rest of the compiler reads a value's kind, and `bytes`, a format of
string, to the `ValueType`. Such a member is kept, the members lowering as written and the `type`
kept verbatim under `openapi:type` (`degraded_lowering`) with the info at the schema: the document
contradicts itself, as unquoted numbers under `type: string` do, and keeping either side alone would
guess which statement it means, where keeping both loses nothing. A `type` beside an `Enum` with no
member types no value, and draws nothing. An enumeration an `Enum` cannot hold — a list or object
member, or members of different kinds — lowers to a `Union` of `Literal`s over its entries but
`null` instead, reported as an info `openapi/degraded-construct` at the schema, and a member whose
value cannot convert makes the enumeration such a union, its own variant the top type, reported as
an `openapi/degraded-construct` warning at the member. That union is exclusive unless an instance
can match two variants: an entry it holds repeats, or one lowered to the top type. `enum: []` lowers
to a closed `Enum` with no members, a position accepting no value, reported as an
`openapi/empty-enum` warning. An entry repeating an earlier entry's value, compared as JSON Schema
compares instances, is kept, each occurrence a member of its own ([enums](../ir/enums.md)), and
reported as an `openapi/duplicate-enum-value` warning at the repeat. An `enum` past
`Limits.MaxEnumMembers` lowers to the top type, with an `openapi/budget-exceeded` error at the
schema. A `const` lowers to a `Literal` of its value, whose `ValueType` is read as an enum's is,
from the type and format the value fits, else the type, else the value's own kind
([containers](../ir/containers.md#literals)), so a `type` or `format` the value fits is held and
draws nothing, and one it does not fit is kept verbatim with the same info report. A `const` of a
value no declared `type` admits matches no instance, keeps its `Literal` and is reported as an
`openapi/degraded-construct` warning at the `const`. A `const: null` lowers as `enum: [null]` does,
to an `Enum` with no member, its null the reference's, so no `Literal` holds a null
([containers](../ir/containers.md#literals)), and where a keyword beside it excludes null it is
reported as that enum's entry is, at the `const`. A `const` that cannot convert lowers to the top
type, with an `openapi/degraded-construct` warning at the schema. A value set an `allOf` refines is
typed by every conjunct ([`allof-refinement`](#allof-refinement)). A `$ref` into a member of such a
union names the member's `Literal`, the node the enumeration lowers at that position, whichever is
lowered first.

- Cases: `enum-string`, `enum-numeric`, `empty-enum`, `enum-member-identity`, `nullable-enum-31`,
  `enum-null-member`, `literal-const`
- Codes: `openapi/empty-enum`, `openapi/duplicate-enum-value`, `openapi/degraded-construct`,
  `openapi/budget-exceeded`
- Tests: `schema.TestEnum_HeterogeneousBecomesUnionWithBadValue`,
  `schema.TestEnum_NonScalarAndMidListMismatch`, `schema.TestEnum_NullIsNeverAMember`,
  `schema.TestEnum_NullAgreesWithTheOneOfSpelling`,
  `schema.TestEnum_ValueTypeReadsTheFormatItsMembersFit`,
  `schema.TestEnum_AMemberNoTypeAdmitsIsReportedAndKept`,
  `schema.TestLiteral_ValueTypeReadsTheTypeAndFormatItsValueFits`,
  `schema.TestConst_NullLowersAsAnEnumOfNullAlone`,
  `schema.TestEnum_ReportsWhyAFormatStayedVerbatim`,
  `schema.TestScalar_AnEncodedFormatBesideAValueSetLowersAsByteDoes`,
  `schema.TestEnum_NarrowsARedeclaredPrimitiveInEitherOrder`,
  `schema.TestEnumAsUnion_UnconvertibleMemberBecomesAny`,
  `schema.TestEnumAsUnion_ExclusiveWhereNoInstanceMatchesTwoEntries`,
  `schema.TestNullableInference_AnEnumsNullMemberFollowsTheReadingInForce`,
  `e2e.TestCompile_AReferenceLeavesItsTargetToItsOwnerInEitherOrder`

## 6. Schemas: objects, arrays and scalars

### `additional-properties`

`additionalProperties: false` lowers to `Model.Additional` closed, and `additionalProperties: true`
or a schema to `Model.AdditionalProps`, whose `Value` types every key no property names. An object
whose one member fact is that catch-all lowers to a `MapT` instead
([containers](../ir/containers.md)). `unevaluatedProperties: false` lowers to `Additional`
closed after composition where no `additionalProperties` is written beside it. An
`additionalProperties` of any value evaluates every property that `properties` and
`patternProperties` leave (JSON Schema 2020-12 §10.3.2.3), which leaves `unevaluatedProperties`
nothing to apply to (§11.3), so beside one the object's openness is `additionalProperties`' alone,
closed for `false` and a catch-all for anything else, and the inert keyword is kept verbatim with it
([`validation-only`](#validation-only)); `{type: object, additionalProperties: {…},
unevaluatedProperties: false}` is a `MapT`. `minProperties` and `maxProperties` lower to the
`Model`'s `Constraints`, which bound the property set's cardinality as against `Additional`'s
openness, or to a `MapT`'s, which bound its entry count, and to the node alone: a property, a
parameter or a header naming the node holds neither ([`constraints`](#constraints)). Written with no
`type` beside a union whose branches each declare an object, either is a body the union distributes
into ([`union-beside-structure`](#union-beside-structure)): each variant is a model over its branch
holding the closure or the catch-all, as beside `type: object`.

`true` writes a catch-all; an absent keyword does not. In JSON Schema, `additionalProperties: true`
is the empty schema, so it types the catch-all as the top type exactly as `{}` does: alone it is a
`MapT` of the top type, and beside properties an `AdditionalProps` of the top type. An absent
`additionalProperties` states nothing, so `{type: object}` stays a `Model` with no properties and
`Additional` unspecified; whether such an object is a free-form map is a target's policy, not a fact
the source declared. Beside `patternProperties` the catch-all's `Value` types a key no pattern
matches ([catch-alls](../ir/models.md#catch-alls)): absent, such a key is unconstrained, so it is
the top type, and the IR does not tell an absent `additionalProperties` from `true`; `false` admits
no such key, so the catch-all holds the patterns and no `Value` beside `Additional` closed. Beside
a live `unevaluatedProperties: false`, with no `additionalProperties`, such a key is admitted only
where composition evaluates it, and the source writes no schema for it, so the catch-all holds the
patterns and no `Value` beside `Additional` closed after composition. That object stays a
`Model`, since a `MapT` has no field for a pattern-scoped value.

`additionalProperties` reads the `properties` and `patternProperties` of the schema object writing
it, and no other's (JSON Schema 2020-12 §10.3.2.3), so a composing `Model` takes its openness and
catch-all from one schema object of the `allOf`: the position's own where it writes
`additionalProperties` or `patternProperties` and no entry declares a property or a pattern, else
the one inline entry writing either where no other object declares one, which the merge then
consumes ([`allof-inline-residue`](#allof-inline-residue)). A `$ref` entry's target declares
nothing here, and what is written beside the `$ref` does. So `{allOf: [{$ref: User},
{additionalProperties: false}]}` closes the `Model` over `User` as `{allOf: [{$ref: User}],
additionalProperties: false}` does, and an entry closing its own `properties` closes the model
holding them. A closure no object takes is kept: beside the `allOf` under
`openapi:additionalProperties` (`degraded_lowering`) with an info `openapi/degraded-construct` at the
position, and on an entry as that entry whole. It applies to the members another object declares
too, so the schema refuses every instance setting one of them that the closure does not admit, as
`{allOf: [{properties: {a: …}}], additionalProperties: false}` admits no object with an `a`, and it
cannot mean a model closed over them: typing it would close or type members it never read.
Two entries writing `additionalProperties` beside no closure are both kept, since the catch-all
would be what both admit. `patternProperties` beside the `allOf` types the catch-all whatever the
entries declare, as a pattern applies to every name it matches and reads no `properties`
(§10.3.2.2). An `unevaluatedProperties: false` beside the `allOf` is inert beside an entry closing
the model with an `additionalProperties`, which evaluates every member, and is kept as validation
logic; beside one writing patterns alone it closes the model after composition.

- Cases: `maps`, `constraints`, `allof-position-constraints`, `allof-nested-inline`
- Tests: `schema.TestObject_CatchAllBesideAnotherFactStaysModel`,
  `schema.TestObject_MapAndNothingElseLowersToMapT`,
  `schema.TestObject_AnInertClosureLeavesAMap`,
  `schema.TestModel_UnevaluatedPropertiesClosedAfterComposition`,
  `schema.TestAllOf_ACatchAllLeavesAClosureAfterCompositionInert`,
  `annotation.TestClosesAfterComposition_OnlyWhereNoAdditionalPropertiesStandsBeside`,
  `e2e.TestKindInference_AClosureBesideAUnionOfObjectsDistributes`,
  `e2e.TestKindInference_ACatchAllBesideAUnionOfObjectsDistributes`,
  `schema.TestCloserOf_NamesTheObjectWhoseClosureTypesTheModel`,
  `schema.TestKeepUnclosing_AClosureBesideAnEntrysMembersIsKeptOnce`,
  `e2e.TestEntryClosure_AClosureOfTheOnlyEntryDeclaringMembersIsTyped`,
  `e2e.TestEntryClosure_TheEntryAndTheBesideSpellingsAreOneModel`,
  `e2e.TestEntryClosure_AClosureThatIsNotTheOnlyDeclarationOfMembersIsKeptWhole`,
  `e2e.TestEntryClosure_ABesideClosureReadingAnotherConjunctsMembersIsKept`,
  `e2e.TestEntryClosure_AClosureBesideTheAllOfOfAnObjectWritingItIsTyped`,
  `e2e.TestEntryClosure_APatternBesideTheAllOfIsTypedWhateverItsEntriesDeclare`,
  `e2e.TestEntryClosure_TheClosureBesideTheAllOfIsTheModelsBeforeAnEntrys`,
  `e2e.TestEntryClosure_TwoEntriesWritingAClosureTypeNeither`,
  `e2e.TestEntryClosure_UnevaluatedPropertiesBesideTheClosingEntry`,
  `e2e.TestEntryClosure_ARefEntrysOwnMembersAreDeclaredBesideTheClosure`,
  `e2e.TestEntryClosure_AClosureBesideAUnionReadsTheAllOfAlike`,
  `e2e.TestEntryClosure_AUseRepeatedLeavesTheModelOfTheClosureAlone`,
  `e2e.TestEntryClosure_TheEntryOrderLeavesTheDocumentAlone`
- Status: partly built: the closure is typed beside an applicator kept verbatim that evaluates a
  member, an inline `allOf` branch kept whole for its `additionalProperties` or
  `patternProperties`, `then`, `else` or `dependentSchemas`, so the typed members refuse one the
  source admits (#1904); and what a target does with a model closed after composition is open, as
  no emitter rule reads it yet (#1868).

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
`List`, reported as an info `openapi/validation-only-keyword`: beside an `items` it is inert and
kept with that `items`, and beside `prefixItems` alone it is live, so `Rest`, the top type an
absent `items` reads as, admits elements the entry refuses unless composition evaluates them, and
the entry is what closes the tuple. The same holds of a `List`'s `Elem`.

JSON Schema drafts 4, 6 and 7 and 2019-09 write a tuple as `items` holding a list, its rest as the
`additionalItems` beside it. In a schema read in one of them
([`dialect-keywords`](#dialect-keywords)) the list lowers to a `Tuple`'s `Elems`, one per entry,
each a schema whose `$ref` is bound, and `additionalItems` to its `Rest` as 2020-12's `items` does
beside `prefixItems`: `false` leaves none, and an absent one reads as `true`. A written
`additionalItems` leaves an `unevaluatedItems` beside the list inert, as that `items` does, and an
absent one leaves it live. Each entry is named as
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

Where such a draft's schema writes 2020-12's `prefixItems` beside its own tuple, the list is the
tuple, since it is the dialect's own spelling, and no text makes the two one tuple: `prefixItems` is
kept verbatim under `openapi:prefixItems` (`degraded_lowering`) with an info
`openapi/degraded-construct` at it, besides the warning
[`later-version-construct`](#later-version-construct) gives a keyword 2020-12 added. Beside one
schema of `items`, which the draft reads as no tuple, `prefixItems` is the tuple, as in 2020-12.

In any other dialect a list of items is no schema: the rule table reports it as an
`openapi/invalid-items-list` error, at every schema the [`validation`](#validation) check reads,
saying what a document of its version writes instead, and the lowering reads `items` as absent; an
`additionalItems` beside it is kept as at a position lowering to no `Tuple`.

- Cases: `tuples-prefixitems`, `schema-drafts`
- Codes: `openapi/validation-only-keyword`, `openapi/invalid-items-list`,
  `openapi/unknown-schema-keyword`, `openapi/validation`, `openapi/degraded-construct`
- Tests: `schema.TestLower_TupleRest`, `e2e.TestOlderDrafts_AListOfItemsIsATuple`,
  `e2e.TestOlderDrafts_AMalformedTupleIsReportedAsItsTwinIs`,
  `e2e.TestOlderDrafts_ATypeListsArrayVariantIsTheTupleWritten`,
  `schema.TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise`,
  `bind.TestTuples_AListOfItemsHoldsSchemas`, `load.TestLandingTargets`,
  `oas.TestCheck_AnItemsListFollowsTheSchemasRows`, `schema.TestLower_PrefixItemsBesideAListedTupleIsKept`
- Status: open: whether a `Rest` or an `Elem` beside a live `unevaluatedItems: false` is typed
  rather than left to the entry (#1883).

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
reasonable degradation), and an applicator written with no type at all where the kind-inference
policy is off, where the position lowers to the top type, which says strictly less than the
applicator does (`{items: {type: string}}` constrains every array instance).

A schema naming no kind, one writing no `type` or a null one and no family
[`competing-keywords`](#competing-keywords) elects first, lowers as the same schema writing the
`type` the keywords giving its instances a type speak of, under the kind-inference policy
([`policies`](#policies)): the Go option `Options.KindInference`, whose zero value is on, and the
text option `kind-inference`, which `false` turns off. A keyword JSON Schema applies to instances of
one kind alone, passing an instance of any other (JSON Schema Core 2020-12 §7.6.1), speaks of that
kind: `properties`, `patternProperties`, `additionalProperties`, `required` and `discriminator` of
an object, `items` and `prefixItems` of an array. A `properties`, a `patternProperties`, a
`required` and a `prefixItems` speak where they list a member, and an `additionalProperties` and an
`items` where they are written and not null, `true` and `{}` among them, so a `properties` written
null or with no member, an empty `required` and a null `items` speak of none. Only a keyword giving
the type of a member, an element or a value infers its kind: a `properties` or a
`patternProperties` listing a member, a `prefixItems` listing a position, a list of `items`, and an
`additionalProperties` or an `items` holding a schema that writes a keyword. A `required` and a
`discriminator` type nothing, nor does a closure, `false`, or a `true` or `{}`, of which Core says
omitting the keyword "has the same assertion behavior as an empty schema" (§10.3.1.2, §10.3.2.3): a
schema writing only such keywords lowers as with the policy off, to the top type keeping them, and
beside a keyword that infers they bind the node it builds, as a `required` names a member of the
model a `properties` makes. Keywords inferring both kinds lower as `type: [object, array]` beside
them does ([`type-arrays`](#type-arrays)): a
`Union` of the object's node and the array's, in that order whichever keyword is written first. The
kind is the policy's reading of what the document means, so the node it builds, at a component, a
property, an element, a body or a union's branch alike, and each variant of such a union, names
`kind-inference` in its `Provenance.Inferred`, and nothing is reported, as the document leaves the
kind unsaid. That node excludes the instances of another kind the schema admits, a string under
`{properties: …}` among them, which is why the reading is a policy and can be turned off. A
keyword's kind is a claim of the position, never a narrowing of the schema object writing it: the
conjuncts of an `allOf`, a `$ref` and a union apply together, and each passes an instance of a kind
its keywords do not speak of, so `{allOf: [{type: string}, {items: {type: string}}]}` admits every
string. The run's reading of what a schema admits ([`constraints`](#constraints)) conjoins first
what the position declares, its `type` and value sets and those of each conjunct, a `$ref`'s target
and a union's branches among them, and lowers to that wherever it names a kind; only where nothing
declares one do the claims decide, the keywords' own united with those of each conjunct whose kind
is a claim, and only then is the node marked. So that schema is a use of the string keeping its
entry with an info, as with the policy off, and a `$ref` whose target the policy reads as kinds
leaving out one the declarations name is no principal ([`allof-refinement`](#allof-refinement)). An `allOf`
composed as a model where no conjunct declares a kind, as one over two `$ref`s to schemas writing
`properties` alone, is marked. Split over entries, keywords of both kinds part from their flat
spelling: `{allOf: [{properties: …}, {items: …}]}` admits an object and an array, so it is no model
but the top type keeping each entry, where `{properties: …, items: …}` is the marked union. A reader asking whether a position
already says what a keyword does, as a union lowered alone restating the body's `type` or a body
whose union branches restate it, reads the kind what it compares to lowers to, whether its
conjuncts declare it or the policy reads it: keywords written with no `type` beside branches each
declaring an object are that object, so branches restating it only narrow the body under either
policy, as beside `type: object`. A bound
beside such a schema is placed by the kind it lowers to, and the reading decides whether the schema
names no kind. Where a conjunct beside it names kinds, as a union kept
beside it does ([`union-beside-structure`](#union-beside-structure)), the keywords name a kind only
where every kind the conjuncts admit is one they speak of, and the schema lowers to those: unmarked
where the document declared them, as `{oneOf: [{type: object, …}], additionalProperties: false}`
declares an object, and marked where a conjunct names its own by the policy's reading, as a branch
writing `properties` alone does. Where the conjuncts admit a kind the keywords do not speak of
(`{oneOf: [{type: object, …}, {type: string}], required: [a]}`), the keywords would narrow what the
document declares, so under either policy the schema is the top type keeping them. A union that
distributes across a body the policy read as an object
([`union-beside-structure`](#union-beside-structure)) marks each variant it composes over a branch
that declares no object, as that variant is a model only by the policy's reading. With the policy
off, a schema naming no kind is the top type, its shape keywords kept as any unhomed keyword is.

Each unhomed keyword is kept verbatim in `Unmodeled["openapi:<keyword>"]` under `degraded_lowering`,
reported as one info `openapi/degraded-construct` at the position naming the kind it lowered to and
the keywords it cannot hold. Which keyword has a home is asked of the node the lowering produced,
never re-derived from the dispatch that produced it. A position that reduced to a shared node gets
an alias of its own first: a shared primitive must never carry one declaration's keywords. The
`format` keyword joins the list wherever the lowered node does not pair it with a primitive — no
`type` declared, or an object, an array or an enum — since it is homed by pairing with a type rather
than by a node kind, and `password` is never unhomed ([`password`](#password)). A format on an array
is kept so whatever its name, `format: set` among them, which some documents write to mean distinct
items and no specification defines: reading it as `uniqueItems` would be a heuristic, which only an
injectable policy may apply, and the entry it is kept under is what such a policy would read. The
census reads
`type`, `const`, `enum`, `minItems`, `maxItems` and `uniqueItems` beside these, so a collection
bound on an object, a scalar or a position with no type is kept the same way, and a keyword the
elected lowering of [`competing-keywords`](#competing-keywords) never reads is too. `discriminator`
joins the list, homed by a `Model` or `Union` that routes by it and by nothing else, so one beside
any other node — the top type, an enum, a literal, a scalar, an alias over a `$ref` — is kept the
same way, its values still read as references ([`discriminator`](#discriminator)), and so is one
naming the empty string as its tag on a `Model`, or a `Union` a `oneOf` or `anyOf` builds, that
cannot hold that tag, whose report says so, where the `Union` an enum or a type array lowers to
routes by none and is reported as for any tag. At a
`$ref` position the census runs on the alias the use-site rule of
[models](../ir/models.md#composition-entries) hoists, since the siblings bind the position rather
than the referent. Every outcome of a schema runs it, and each says what it lowered to. A union
holds what a node of its kind holds. A collapsed union and an expanded `$dynamicRef` lower to a
reference, so they are read as a `$ref` position is, and an `allOf` that refines takes the same
census, but for what its principal already says, a union beside it among what it keeps
([`allof-refinement`](#allof-refinement)). A
multi-typed schema (`type: [string, object]`) and a union distributed over its branches are built
from the one schema per variant, so the census asks the variants, and a keyword is homed where any
holds it, a value bound where a variant whose type takes it does: a distributed union's `properties`
are consumed, and a `format` or `maxItems` beside them is kept on the union, under a report naming a
union of models.

- Cases: `unhomed-keywords`, `empty-enum`, `keywords-at-every-outcome`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestObject_ACatchAllBesideARequiredNameStaysAModel`,
  `e2e.TestKindInference_AnUntypedPropertiesSchemaIsMarked`,
  `e2e.TestKindInference_AnInlineSchemaIsMarkedAtEachPositionOwningANode`,
  `e2e.TestKindInference_ANullTypeStillInfersTheKind`,
  `e2e.TestKindInference_ANullableWithNoTypeStillInfersTheKind`,
  `e2e.TestKindInference_ATypedSchemaIsNeverMarked`,
  `e2e.TestKindInference_AKindTheSchemaNamesKeepsItsStrayKeywordsUnmarked`,
  `e2e.TestKindInference_ANullOrEmptyShapeKeywordClaimsNoKind`,
  `e2e.TestKindInference_DisabledLowersAnUntypedShapeToTheTopType`,
  `e2e.TestKindInference_DisabledReportsEveryKeywordOfASchemaOnce`,
  `e2e.TestKindInference_TheTextOptionSwitchesThePolicy`,
  `e2e.TestKindInference_AnUntypedShapeLosesNothingUnderEitherPolicy`,
  `e2e.TestKindInference_APropertySetBesideAUnionComposesByThePolicy`,
  `e2e.TestKindInference_EveryTypingKeywordInfersTheKindItSpeaksOf`,
  `e2e.TestKindInference_AnInferredCollectionKeepsItsMemberTypes`,
  `e2e.TestKindInference_AnInlineCollectionIsMarkedAtItsPosition`,
  `e2e.TestKindInference_ASchemaSpeakingOfTwoKindsIsAMarkedUnionOfBoth`,
  `e2e.TestKindInference_AThreeZeroSchemaSpeakingOfTwoKindsIsAMarkedUnion`,
  `e2e.TestKindInference_EachVariantOfATwoKindUnionIsMarked`,
  `e2e.TestKindInference_ATwoKindUnionIsOneInEitherKeywordOrder`,
  `e2e.TestKindInference_AKindAConjunctNamesIsDeclared`,
  `e2e.TestKindInference_KeywordsNarrowingAKeptUnionNameNoKind`,
  `e2e.TestKindInference_APropertySetBesideAUnionOfOtherKindsIsTheTopType`,
  `e2e.TestKindInference_AKindReadThroughAnInferredBranchIsMarked`,
  `e2e.TestKindInference_AVariantComposedOverAnInferredBodyIsMarked`,
  `e2e.TestKindInference_ADeclaredKindDecidesBesideAnEntryTheKeywordsClaim`,
  `e2e.TestKindInference_TheEntryOrderAndThePolicyLeaveTheDeclaredKindAlone`,
  `e2e.TestKindInference_ACompositionIsMarkedWhereNoConjunctDeclaresItsKind`,
  `e2e.TestKindInference_AnEntryReadAloneIsItsOwnPosition`,
  `e2e.TestKindInference_AFormBodyKeysNoPartItsModelLacks`,
  `e2e.TestKindInference_WhatAPositionSaysIsReadOffWhatItLowersTo`,
  `e2e.TestKindInference_OnlyAKeywordGivingATypeInfersItsKind`,
  `e2e.TestKindInference_AKeywordTypingNothingStillSpeaksOfItsKind`,
  `e2e.TestKindInference_ANarrowingBesideATypingKeywordStaysOnItsModel`,
  `e2e.TestKindInference_AClosureBesideAUnionOfObjectsDistributes`,
  `e2e.TestKindInference_ACatchAllBesideAUnionOfObjectsDistributes`,
  `e2e.TestKindInference_AClosureBesideAUnionOfOtherKindsIsTheTopType`,
  `e2e.TestKindInference_AClosedFormBodyBesideAUnionKeysNoPartItsModelsLack`,
  `schema.TestAdmissionOf_KindsAreWhatEachConjunctionLowersTo`,
  `schema.TestUnhomedApplicator_KeptOnAListAndAnnounced`,
  `schema.TestUnhomedApplicator_FormatWithNoTypeIsKept`,
  `schema.TestUnhomedApplicator_AFormatOnAnArrayIsKeptNotRead`,
  `schema.TestPreserveUnknownKeywords_ReadsWhatADraftSpellsOtherwise`

### `string-formats`

A `type: string` with a `format` the table names lowers to the shared primitive the pairing selects,
and no node of its own: `date` to `date`, `time-local` to `time`, `duration` to `duration`, `uuid`
to `uuid`, `uri` to `url`, `date-time` to `datetime_offset`, `date-time-local` to `datetime`,
`binary` to `bytes`. A format whose value is of another primitive, written in a text other than
that primitive's own, lowers to a hoisted `Scalar` over that primitive whose `Encoding` names the
text as the [IR](../ir/constraints.md#encodings) does: `byte` over `bytes` under `base64`, and
`base64url` over `bytes` under `base64url`, each with the `string` it travels as in `WireType`, and
`http-date` over `datetime_offset` under `http-date`, with no `WireType`, since an instant already
travels as a string. `unixtime` on a string is [`numeric-formats`](#numeric-formats)'. A numeric
format the table pairs with `number` ([`numeric-formats`](#numeric-formats)), such as `int64`,
`decimal` or `double`, names a number written as its text, as a value too wide for a JSON number's
readers may travel, so it lowers the same way: to a hoisted `Scalar` over the primitive it selects
under `number`, whose `Encoding` is `string`, a number written as the text of its value, with the
`string` in `WireType`. The base is the logical type and `WireType` the wire's, for each, as
[encodings](../ir/constraints.md#encodings) orients the triple. Any other format (`time`, `email`,
`uri-reference`, `ipv4`, an invented one) lowers to a hoisted `Scalar` over `string` whose
`Encoding.Name` is the format as written, case kept, with no `WireType`, since the compiler knows no
registry of formats beyond these, and the table leaves out `time`, RFC 3339's `full-time`, because
it carries an offset and the `time` primitive holds none
([primitives](../ir/primitives.md#offsets)). A `contentEncoding` beside such a format, a numeric or
an unknown one names the encoding instead; where the two name different encodings the format is
kept verbatim under `openapi:format` (`no_ir_home`) with an info `openapi/degraded-construct` at it.
A format of the first kind so kept leaves the primitive and the `WireType` it selects in place,
while a numeric format so kept selects no base, since the string is then that encoding's text and
not a number's:
`{type: string, format: int64, contentEncoding: base64}` lowers to a `Scalar` over `string` under
`base64` with no `WireType`, as the encoding alone does
([`content-vocabulary`](#content-vocabulary)). A scalar hoisted for its format carries the bounds
written beside it, but for a number carried as text, whose `Constraints` speak of the number
([constraints](../ir/constraints.md#constraints)): a length or a `pattern` bounds its text and a
numeric bound no string instance, so each value bound written beside it is kept verbatim under
`openapi:<keyword>` (`no_ir_home`) on the `Scalar` alone with an info `openapi/degraded-construct`
at it, one an inline `allOf` branch writes keeps its branch whole
([`allof-inline-residue`](#allof-inline-residue)), and neither the `Scalar` nor a property or
parameter carrying it holds one. The table is the same at every minor, and a pairing of `format`
with another `type` is [`numeric-formats`](#numeric-formats)'. Beside a type array, a format the
table pairs with `string`, `byte` and `http-date` among them, is the `string` variant's alone
([`type-arrays`](#type-arrays)).

- Cases: `encoding-byte`, `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestLower_ATypeArraysFormatIsTheVariantsItPairsWith`,
  `schema.TestScalar_UnknownFormatPerBaseType`,
  `schema.TestScalar_EncodedFormatPairings`,
  `schema.TestScalar_AnEncodedFormatKeepsTheBoundsWrittenBesideIt`,
  `schema.TestScalar_AnEncodedFormatBesideContentEncodingKeepsItsPrimitive`,
  `schema.TestScalar_ANumberAsTextIsItsNumberOnAString`,
  `schema.TestScalar_ANumberAsTextKeepsItsBoundsVerbatim`,
  `schema.TestLower_NamedScalarComponentResolves`,
  `merge.TestKindOf_ANumberAsTextIsAString`,
  `spelling.TestFormatTable_EveryRowDistinguishesTheBareType`,
  `spelling.TestFormatTable_EveryStringRowLowersToItsPrimitive`,
  `spelling.TestEncodedTable_IsApartFromTheFormatTable`
- Status: partly built: a value bound beside a `$ref` to a number carried as text, or beside an
  `allOf` refining one, lands in the `Constraints` of the alias over it, and a length or `pattern`
  beside `byte`, `base64url` or `http-date` in those of its `bytes` or `datetime_offset`, which
  speak of the bytes or the instant, though it bounds the text they travel as (#1851).

### `numeric-formats`

A numeric `type` with a `format` the table names lowers to the primitive the pairing selects, and no
node of its own: an integer width — `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`,
`uint64` — under `integer` and `number` alike, since OpenAPI applies a format to both (3.1.1
§4.4.1), and `float`, `double`, `decimal` and `decimal128`, to `float32`, `float64`, `decimal` and
`decimal128`, under `number` alone, since under `integer` they admit only integral values, which no
primitive narrower than `integer` holds. `unixtime`, seconds since the epoch, on a `number`, an
`integer` or a `string`, lowers to a hoisted `Scalar` over `datetime_offset` whose `Encoding` is
`unixTimestamp`, the name the [IR](../ir/constraints.md#encodings) gives that text, with the type it
is written on in `WireType`, which tells whole seconds from a fraction and from a decimal text, as
[`string-formats`](#string-formats) lowers `byte`. A pairing the table does not name lowers to a
hoisted `Scalar` over the type's primitive that keeps the format as `Encoding.Name`. A
`contentEncoding` beside a `string`'s `unixtime` takes that field, and the format is kept verbatim
under `openapi:format` (`no_ir_home`) with an info `openapi/degraded-construct` at it; beside a
number's format it applies to strings alone and takes nothing from it
([`content-vocabulary`](#content-vocabulary)). Beside a type array, a format the table pairs with a
listed number is that variant's alone, so a width beside `integer` and `number` is both variants'
([`type-arrays`](#type-arrays)).

- Cases: `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_NumericFormatPairings`,
  `schema.TestScalar_EncodedFormatPairings`,
  `schema.TestContentVocabulary_KeptWhereNoEncodingHolds`,
  `schema.TestContentVocabulary_ANumberTakesNone`,
  `schema.TestLower_ATypeArraysFormatIsTheVariantsItPairsWith`
- Status: partly built: a numeric bound beside `unixtime` on a `number` or an `integer` lands in the
  `Scalar`'s `Constraints`, which speak of the instant, though it bounds the seconds it travels as
  (#1851).

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

### `secret-inference`

The secret policy, `Options.SecretInference` and the text option `secret-inference`
([`policies`](#policies)), redacts a value whose name says it is a credential where the document
asks nothing about its redaction. A parameter, a header of a response or of an encoding, or a
model's member is `Secret` where its name's canonical words, a number after them aside, end in
`password`, `passwd`, `passphrase`, `secret` or `apikey`, in `pass` followed by `phrase`, in `api`,
`access`, `secret` or `private` followed by `key`, or in `access`, `refresh`, `auth`, `bearer`,
`session` or `api` followed by `token`, or are `authorization` or `proxy` `authorization` alone, the
fields whose value RFC 9110 defines as credentials (§11.6.2, §11.7.2), and its type reads, through
its scalars, as `string` or `bytes`. A name's last words decide what it names, so `secret_id`,
`R2-Access-Key-Id`, `page_token` and `payment_authorization` name no credential while `password2`
and `passPhrase` name one, and `token` alone names none, since one API calls a credential so and
another a unit of text; a value of another type, as `has_password: {type: boolean}` or a `secret`
object holding a credential's parts, is none either. Such a name on such a type is the policy's
trigger, and it declines nothing that meets it: the document has spoken where the use is already
`Secret` or its type `Sensitive` ([`password`](#password)), and where a redaction key the promotion
maps is written on the use or on a node its type reads through, so `x-sensitive: false` keeps a
credential's name from asking. What it fills the document leaves silent, so nothing is reported. The
reading waits for the document to be whole, since a `$ref` can name a type built after its use. On
the golden spec, of the 123 uses its words and types match, the document redacts 93 itself, and the
policy marks the other 30: the 2 `R2-Secret-Access-Key` header parameters and 28 members, 17
`authorization`, 4 `client_secret`, 3 `password`, 2 `secret_access_key`, a `build_token_secret` and
an `r2_secret`. One of the `password` members, a leaked-credential detection's, holds the ruleset
expression that finds a password in a request rather than a password, which its name cannot tell
apart (spec @86556f6f).

- Cases: `secret-inference`
- Tests: `convention.TestRedact_ReadsTheNamesTail`, `convention.TestRedact_ReadsTheValuesType`,
  `convention.TestRedact_LeavesWhatTheDocumentSays`, `convention.TestRedact_ReachesEveryUse`,
  `promotion.TestPromoter_WritesSeesAKeyWhateverItSays`,
  `e2e.TestConventions_DeclarationOrderChangesNothing`, `e2e.TestConventions_DisabledFillsNothing`

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
([`null-schema-is-false`](../parser-library.md#null-schema-is-false)), which admits no value. So is
any other value that is neither a mapping nor a boolean, a string, a number or a list, which the
load phase reports as the wrong type where the source's grammar reads a schema, and which a `$ref`
naming that position reads alike; one a `$ref` reaches in raw YAML or another document is reported
as that `$ref`'s `openapi/unresolved-ref` instead ([`references`](#references)), and the `$ref`
lowers the top type. Only a written `true` or `false` is a boolean schema. An
entry, which exists whatever its value — a property, a composition branch, a component, a
`prefixItems` position, a parameter's, header's or media type's `schema` — lowers as the empty
schema does: to the top type, or as an `allOf` branch to nothing composed, and a `$ref` naming it as
one naming `{}` does. A keyword whose value is a schema, whose absence states something its empty
schema may not, is read as left out: `additionalProperties: null` writes no catch-all and leaves
an `unevaluatedProperties` beside it live, `contentSchema: null` writes no `Encoding.Schema`, and a
`null` `not`, `if`, `then`, `else`, `contains`, `propertyNames`, `unevaluatedItems` or
`unevaluatedProperties` is kept nowhere. `items: null` lowers as an absent `items` does, leaving an
`unevaluatedItems` beside it live, and a `null` `items` or `additionalProperties` a node has no home
for is not kept for want of one ([`shape-applicator`](#shape-applicator)). Nothing is kept verbatim,
since a `null` means nothing a consumer could recover, and the lowering reports nothing of its own,
since reading it so loses nothing: the load phase reports each such `null` at the node
([`validation`](#validation)), an entry's as an error and a keyword's as the warning a field left
out draws, but for `items` beside a 3.0 `type: array`, whose absence
[`missing-array-items`](#missing-array-items) reports instead. One inside a value the IR keeps
verbatim whole, a `dependentSchemas` entry, stays in that value, since nothing reads it as a schema.

- Tests: `e2e.TestCompile_ANullWhereASchemaBelongsIsNoSchema`,
  `schema.TestNullSchema_AnEntryIsTheEmptySchemaAndAKeywordIsLeftOut`,
  `statement.TestSub_TruthTellsNoSchemaFromFalse`,
  `e2e.TestCompile_AValueWhereASchemaBelongsThatIsNoMappingIsNoSchema`

### `null-keywords`

A `null` written for a keyword of a Schema Object is read as that keyword left out, whatever the
keyword and whatever node the schema lowers to, as [`null-schema`](#null-schema) reads one whose
value is a schema: no keyword admits it, so it states nothing, and the schema lowers exactly as it
does with the key removed. A `null` bound draws no `openapi/invalid-numeric-literal`, `type: null`
declares no type, unlike `type: "null"`, and nothing is kept verbatim for want of a home. `const`,
`default` and `example` hold an instance, of which `null` is one, so a `null` there is that value
([`enum-const`](#enum-const), [`nullable`](#nullable)), as an `enum` member is. A keyword of no
vocabulary has no type to break and is kept with the `null` it wrote, as
[`unknown-keys`](#unknown-keys) keeps any value. An empty dependency map is read the same way
([`validation-only`](#validation-only)). The load phase reports each such `null` at the node
as the warning a field left out draws ([`validation`](#validation)); the lowering reports nothing
of its own, since reading it so loses nothing.

- Cases: `null-keywords`
- Codes: `openapi/invalid-numeric-literal`
- Tests: `e2e.TestCompile_ANullKeywordLowersAsTheKeywordLeftOut`,
  `e2e.TestCompile_ANullIdentifierLowersAsTheKeywordLeftOut`,
  `statement.TestSchema_KeyReadsANullKeywordAsLeftOut`,
  `bind.TestDynamicAnchor_ANullAnchorNamesNothing`,
  `oas.TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional`,
  `e2e.TestCompile_ANullBeneathItsOwnNameLowersAsTheKeywordLeftOut`

### `null-alone`

A schema whose own keywords leave it no value but `null` lowers, at its own position, to a closed
`Enum` with no member, the empty value space ([the empty enum](../ir/enums.md#the-empty-enum)), and
the reference naming it admits `null` where the schema does ([`nullable`](#nullable)): a position
admitting `null` and nothing else is a nullable reference to that `Enum`, and one admitting nothing
is a reference to it that is not. Its own keywords leave it `null` alone where it writes no `$ref`,
`allOf`, `oneOf`, `anyOf` or `$dynamicRef`, an `enum` it writes lists `null` and nothing else,
within the `Limits.MaxEnumMembers` past which an `enum` lowers to the top type, and it writes a
`const`, such an `enum`, or a `type` naming `null` and no other type, as `type: "null"` and
`type: ["null"]` do. An `enum` and a `const` lower so by [`enum-const`](#enum-const). A `type`
naming only `null`, with neither beside it, lowers so too, not to the top type behind a nullable
reference, which admits every value: `{anyOf: [{}, {type: "null"}]}` lowers to that, and a consumer
could not tell the two apart. The `Enum`'s `ValueType` is what the schema's `type` and `format`
select, as an enumeration's is, the top type where they select none. A keyword the `Enum` has no
field for, such as `properties`, `items`, a `format` or a bound, is kept by the census
([`shape-applicator`](#shape-applicator)) under `openapi:<keyword>` (`degraded_lowering`), with an
info `openapi/degraded-construct` at the schema, and the `type` draws nothing, an `Enum` with no
member typing no value. A schema writing a `$ref`, a union or an `allOf` admits what they say as
well as what its own keywords do, so it lowers as they decide: a `$ref` to a schema of `null` alone
names that schema's `Enum`, nullable as the reference reads it ([`ref-use-site`](#ref-use-site)),
and an `allOf` of one alone refines to it ([`allof-refinement`](#allof-refinement)). A union's
branch of `null` alone, written in place, is that union's `null` rather than a variant
([`union-null-variant`](#union-null-variant)), so each spelling of `null` alone lowers to the one IR
wherever it stands.

- Cases: `null-alone`, `enum-null-member`, `null-only-union`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestNullAlone_ReadsTheSchemasOwnKeywords`,
  `schema.TestNullAloneReading_IsTheWholeSchemasReading`,
  `schema.TestNullAloneReading_ReadsASchemaOnce`,
  `schema.TestNullBranch_AnEnumPastTheMemberBudgetIsNoNullBranch`,
  `schema.TestLowerNullAlone_EveryPositionNamesAnEnumWithNoMember`,
  `schema.TestLowerNullAlone_IsToldFromTheTopType`,
  `schema.TestLowerNullAlone_KeepsWhatTheEnumCannotHold`,
  `schema.TestRefinement_ANullOnlyBranchIsTheNull`

## 7. Schemas: keywords beyond the shape

### `constraints`

`minimum`, `maximum` and `multipleOf` lower to `Min`, `Max` and `MultipleOf`, and `minLength`,
`maxLength` and `pattern` to `MinLength`, `MaxLength` and `Pattern` as written, in the `Constraints`
of the node the position owns (a `Scalar`, the `Union` a `oneOf` or `anyOf` lowers to), or of its
carrier, a `Property` or a `Parameter`, where it owns none; `minItems`, `maxItems` and `uniqueItems`
to the `List`'s, and a `Tuple`'s per [`tuples`](#tuples), or, where the position lowers to a
reference (a `$ref` site, a collapsed union or an expanded `$dynamicRef`), to the `Constraints`
of the alias or carrier it owns where the types the position admits include `array`, as does one on
an inline `allOf` branch ([`allof-inline-residue`](#allof-inline-residue)); and
`minProperties` and `maxProperties` per [`additional-properties`](#additional-properties). A bound
is read only where a type the position admits takes it, since JSON Schema applies each to
instances of one kind and lets any other through (JSON Schema Validation 2020-12 §6.2 to §6.5):
the numeric bounds, the exclusive ones among them, where it admits `integer` or `number`,
`minLength`, `maxLength` and `pattern` where it admits `string`, the collection bounds where it
admits `array`, and `minProperties` and `maxProperties` where it admits `object`; `boolean` and
`null` take none. The types a position admits are those its node lowers from: the kinds its own
`type` declares and the members of its `enum` or `const` have, that every schema it names admits
too: what its `$ref` lands on, the keywords beside each `$ref` of the chain read with it, each
`allOf` entry, a `oneOf` or `anyOf`, which admits what any of its branches does, a branch of null
alone, written with a `type` or not, admitting none a bound applies to
([`union-null-variant`](#union-null-variant)), and the target a `$dynamicRef` or `$recursiveRef`
names ([`dynamic-ref`](#dynamic-ref)), read through as many of those as the named schemas write,
from the source and never from what has been lowered, once a compile, so no answer turns on the
order schemas are declared in or on how many the read takes in. Where that leaves every kind, it
admits the kinds the schema's shape keywords speak of under the kind-inference policy, as it lowers
to them ([`shape-applicator`](#shape-applicator)). A read meeting
a cycle of `$ref`s, `allOf` entries or dynamic references, or a `$ref` landing on no schema,
whichever schema of it is read first, leaves the position's own `type` to decide. A schema
declaring no `type` and naming none excludes nothing, so it reads every bound; one declaring `null`
alone reads none, its `Enum` with no member keeping them ([`null-alone`](#null-alone)), and so does
one whose own and named types share no kind. A bound no
type the position admits takes reaches no `Constraints`, the carrier's included, and is kept
verbatim under `openapi:<keyword>` (`degraded_lowering`) with an info `openapi/degraded-construct`
naming what the position lowered to: on the node it owns, on an alias hoisted where it lowered to a
shared node or a reference, or on the carrier of a reference. A type array's variants each take
their own type's, where the position admits it ([`type-arrays`](#type-arrays)). Each named schema
is read in its own document, an inline one at its own pointer, so a dynamic reference names what it
does where that schema is lowered. A bound is read off the raw node, never the parser's `float64`, so
its `BigVal` keeps every digit the literal wrote ([`values`](#values)), a quoted one as the decimal
it spells; one that is no decimal literal a `BigVal` holds is dropped with an
`openapi/invalid-numeric-literal` error at the keyword.
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
errors are the rule table's, made with the rows of each schema's dialect, at every schema its
[`validation`](#validation) check reads, which judges these keywords as the lowering reads them
rather than by their shape: one under `not`, `if` or `propertyNames`, or beside an `enum`, is
reported as one the lowering reads, and an aliased value once. A bound beside a `oneOf` or an
`anyOf` lowering to a `Union` of its branches ([`union`](#union)), or on an `allOf` branch folding
there, is that `Union`'s: no variant can hold it, each being its branch's node, which every use of
the branch shares. A bound has one home: the node its position owns, else the carrier, which holds
one only where its position owns no node, over a reference or a primitive every use shares, else a
node hoisted there ([one home](../ir/annotations.md#one-structural-home-per-declaration)). A
property, a parameter or a header whose position owns its node holds none of its bounds, which a
`$ref` naming that position reads on the node alone; one naming a position that owns none, a shared
primitive's, reads a `Scalar` hoisted there holding them, while the carrier keeps its own. The
node's kind decides what it holds, whatever the position admits: a `Model` or a `MapT` the property
counts alone, as its instances are objects, a `List` or a `Tuple` the collection bounds, and a
`Union` those its variants take, or its variants where the position builds them, a type list's
([`type-arrays`](#type-arrays)) or a distributed union's, each those of the kinds it admits, its
branch's within the body's ([`union-beside-structure`](#union-beside-structure)). A bound its kind does not take, and one
beside a node with no `Constraints` field (an `Enum`, a `Literal`, the `Union` of literals a mixed
`enum` lowers to), is kept verbatim on the node under `openapi:<keyword>` (`degraded_lowering`),
once, with an info `openapi/degraded-construct` naming the node it lowered to, as
[`shape-applicator`](#shape-applicator) keeps a collection bound on an object
([unions](../ir/unions.md#bounds-beside-a-union)), and one an inline `allOf` branch writes there
keeps its branch whole ([`allof-inline-residue`](#allof-inline-residue)). A collection bound beside a reference that
`Constraints` cannot state is kept the same way: a `uniqueItems` of `false`, which a bool cannot
tell from one never written, and a count that does not fit in 64 bits. A count,
`minLength`, `maxLength`, `minItems`, `maxItems`, `minProperties` or `maxProperties`, is a
non-negative integer with no upper limit, and its field holds one up to 2^63−1: one past that, at
any node, carrier or `allOf` branch, reaches no field and is kept verbatim under `openapi:<keyword>`
(`degraded_lowering`) with an info `openapi/degraded-construct` saying so where it is written, and on
a branch it parts no other ([`allof-inline-residue`](#allof-inline-residue)), but for one past what
a `BigVal` holds, which reaches the IR in no form
([`verbatim-preservation`](#verbatim-preservation)). Where several writers
of one position, its schema and the branches folding into it, give one such keyword, the one
binding the rest is kept, the smallest maximum or the largest minimum, compared exactly however
large, the first of one value written twice, with one report. Two declarations of one field are
not such writers: each keeps its own where it is written, and their fold compares the two entries as
it does any kept under `Unmodeled` ([`allof-redeclared-property`](#allof-redeclared-property)), so
of two such counts the later declaration's is kept and the other is reported and kept with its
declaration. A count literal that is no non-negative integer is the rule table's to report
([`validation`](#validation)) and reaches no field, nor is it kept. A number carried as text holds
no bound ([`string-formats`](#string-formats)).

- Cases: `constraints`, `numeric-precision`, `ref-collection-bounds`, `reference-bounds`
- Codes: `openapi/invalid-numeric-literal`, `openapi/invalid-exclusive-bound`,
  `openapi/degraded-construct`
- Tests: `schema.TestConstraints_ABoundIsReadWhereItsTypeTakesIt`,
  `schema.TestConstraints_SeveralBoundsATypeCannotTakeDrawOneReport`,
  `schema.TestConstraints_APositionDeclaringNoTypeReadsEveryBound`,
  `schema.TestConstraints_APositionOfNullAloneReadsNoBound`,
  `schema.TestConstraints_AReferenceOutcomeReadsByTheTypesItNames`,
  `schema.TestConstraints_ABoundIsReadByTheTypesItsSchemaNames`,
  `schema.TestConstraints_ABoundBesideARefIn30IsKeptAsIn31`,
  `schema.TestConstraints_ABoundANamedTypeTakesIsRead`,
  `schema.TestConstraints_ATypeBesideANamedSchemaStillNarrowsTheRead`,
  `schema.TestConstraints_AUnionIsReadByWhatItsBranchesAdmit`,
  `schema.TestConstraints_ABranchBoundIsReadByTheTypesItsSchemaNames`,
  `schema.TestAllOf_ACompositionReadsBoundsByTheTypesItComposes`,
  `schema.TestAllOf_AnEmptyConjunctionKeepsEveryBound`,
  `schema.TestUnion_ADistributedBodyReadsBoundsByTheTypesItComposes`,
  `e2e.TestVariantBounds_AVariantHoldsOnlyTheBoundsItsBranchTakes`,
  `e2e.TestVariantBounds_ABoundOfAKindNoBranchTakesIsNotLostToTheVariants`,
  `e2e.TestVariantBounds_ABoundNoVariantTakesIsKeptOnceOnTheUnion`,
  `schema.TestConstraints_AReadOfAnySizeKeepsTheBoundInEitherOrder`,
  `schema.TestConstraints_AnExpandedReferenceIsNamedByItsKeyword`,
  `schema.TestConstraints_ABoundIsReadTheSameInEitherDeclarationOrder`,
  `schema.TestWritesBound_FindsABoundOnTheSchemaOrAnInlineBranch`,
  `schema.TestConstraints_ABoundBesideAUnionItsTypeExcludesIsKept`,
  `schema.TestAllOf_ABoundBesideABranchsOwnRefIsReadByItsTarget`,
  `schema.TestConstraints_ATypeListReadsWhatThePositionAdmits`,
  `schema.TestConstraints_ANamedSchemasDynamicReferenceIsReadThrough`,
  `schema.TestConstraints_AnInlineBranchIsReadAtItsOwnPointer`,
  `schema.TestBoundKinds_ReadsEachShapeWhole`,
  `schema.TestBoundKinds_ACycleLeavesTheOwnTypeToDecide`,
  `e2e.TestCompile_ABoundBesideAValueSetOfOtherKindsIsKeptNotPlaced`,
  `e2e.TestCompile_ABoundBesideAnInferredObjectIsKeptOnItsHome`,
  `e2e.TestCompile_ABoundBesideAWideAllOfOfStringsIsDecidedAtEverySize`,
  `e2e.TestCompile_AnAllOfCycleBesideABoundIsReadAlikeFromEveryEntry`,
  `operation.TestParams_ABoundBesideAReferenceIsReadByItsNamedType`,
  `e2e.TestConstraints_ANamedTypeIsReadInItsOwnDocument`,
  `schema.TestConstraints_ABoundAListKeepsIsKeptOnce`,
  `schema.TestConstraints_ACarrierOwningItsNodeHoldsNoBound`,
  `e2e.TestCompile_APropertyOverANodeOfItsOwnHoldsNoBoundThatNodeHolds`,
  `e2e.TestCompile_AParameterOverANodeOfItsOwnHoldsNoBoundThatNodeHolds`,
  `e2e.TestCompile_AHeaderOverANodeOfItsOwnHoldsNoBoundThatNodeHolds`,
  `e2e.TestCompile_AReferenceNamingAPropertyReadsTheNodeHoldingItsBound`,
  `e2e.TestCompile_AReferenceNamingASharedPrimitivePropertyReadsANodeHoistedThere`,
  `e2e.TestCompile_ACarrierOverAReferenceOrASharedPrimitiveKeepsItsBound`,
  `e2e.TestCompile_ABoundBesideAReferenceToAModelIsHeldOnce`,
  `e2e.TestCompile_ABoundOfAnotherKindIsKeptOnItsHomeAndBreaksNoInvariant`,
  `e2e.TestCorpus_NoCarrierRestatesABoundItsOwnNodeHolds`,
  `schema.TestVoices_ABoundTheComposedModelCannotTakeKeepsItsBranch`,
  `schema.TestConstraints_ABoundBesideAValueSetOrAUnionHasOneHome`,
  `schema.TestConstraints_AReferenceLandingOnAPropertyReadsItsNode`,
  `e2e.TestStringConstUnion_ABoundBesideTheSetKeepsTheUnion`,
  `schema.TestConstraints_ACountATypeCannotTakeIsKeptByTheCensus`,
  `schema.TestConstraints_AModifierATypeCannotTakeIsKeptByTheCensus`,
  `schema.TestKeywordKinds_ListsEveryValueBoundAndTheContentVocabulary`,
  `schema.TestBoundsFor_ReadsEachBoundForTheKindsItAppliesTo`,
  `schema.TestDeclaredInstanceTypes_ReadsTheTypesASchemaDeclares`,
  `e2e.TestDialectShapes_AreReportedWhereverTheCheckReadsASchema`,
  `oas.TestCheck_BoundsFollowTheSchemasRows`,
  `schema.TestConstraints_ExclusiveBoolean30`,
  `schema.TestConstraints_ExclusiveWrongDialectForm`,
  `schema.TestConstraints_LosslessNumericLiterals`,
  `schema.TestConstraints_ABoundPastTheExponentRangeIsRefused`,
  `schema.TestCollectionBound_BesideAReferenceReachesTheAlias`,
  `schema.TestCollectionBound_BesideAReferenceReachesTheCarrier`,
  `schema.TestCollectionBound_AReferenceKeepsWhatConstraintsCannotState`,
  `schema.TestCollectionBound_ACarrierLeavesItToTheNodeItsPositionOwns`,
  `schema.TestCollectionBound_ANodeOfAnotherShapeKeepsItVerbatim`,
  `schema.TestCollectionBound_IsReadByTheTypesItsSchemaNames`,
  `schema.TestCollectionBound_BesideAnExpandedDynamicRefReachesWhatThePositionOwns`,
  `operation.TestParams_CollectionBoundBesideARefReachesTheCarrier`,
  `schema.TestCollectionBound_ARefinementReachesWhatThePositionOwns`,
  `schema.TestRefinement_ACollectionBoundLandsAtEveryPosition`,
  `schema.TestConstraints_ACountNoFieldStatesIsKeptVerbatim`,
  `schema.TestConstraints_ACountTheLoadPhaseRefusesReachesNoField`,
  `schema.TestConstraints_ACountAVariantKeepsIsKeptOnce`,
  `schema.TestConstraints_AModifierBoundingNothingIsReportedOnce`,
  `schema.TestAllOf_ACountNoFieldStatesIsKeptInEitherOrder`,
  `schema.TestAllOf_TheBindingCountIsKeptInEveryOrder`,
  `schema.TestCompareCounts_OrdersLiteralsExactly`,
  `annotation.TestExactCount_StatesOnlyAnExactCount`,
  `oas.TestShape_Admits`
- Status: partly built: two declarations of one field that each keep a bound meet the fold of
  [`allof-redeclared-property`](#allof-redeclared-property) (#1889). A bound left unread is reported
  at its position, with the census's other keywords, not at the keyword. A bound beside an enum a
  promotion opens, which bounds the values it does not list, is kept verbatim as a closed one's is
  (#2004).

### `validation-only`

JSON Schema's validation-only keywords are kept verbatim under `validation_only`
([boundaries](../ir/boundaries.md#validation-only-constructs)) on the node or carrier the
position owns, one entry per family: `not`, `dependentSchemas`, `dependentRequired` and
`propertyNames` each under `openapi:<keyword>`, at the keyword; and three families each folded into
one object, at the schema — `if`, `then` and `else` under `openapi:if-then-else`, `contains` with
`minContains` and `maxContains` under `openapi:contains`, and `unevaluatedProperties` with
`unevaluatedItems` under `openapi:unevaluated`, every form of each but an `unevaluatedProperties:
false` the node the position lowered to holds as a model's closure
([`additional-properties`](#additional-properties)): a `Model` closed after composition, or a
`Union` whose variants the schema builds itself, a type list's or a distributed union's
([`union-beside-structure`](#union-beside-structure)), one of them such a `Model`. Anywhere else,
on a scalar, an enum, a list, a `$ref` site's alias or carrier, a plain union, a model a `false`
`allOf` branch closed or the top type, that form is kept with the others, so a consumer finds the
family under one key whatever the position lowered to. In JSON Schema these two keywords apply
only to what no keyword beside them evaluates (2020-12 §11.2, §11.3), so one is inert beside the
keyword that evaluates every location it could apply to: an `additionalProperties` of any value
beside `unevaluatedProperties`, and beside `unevaluatedItems` an `items` schema or, where the
dialect reads a list of `items` as a tuple ([`tuples`](#tuples)), the `additionalItems` written
beside the list. An inert keyword is kept with that keyword, written just before it in the entry's
object: `{"additionalProperties": true, "unevaluatedProperties": false}`. The entry's keywords are
read as written in the schema at its position, beside the keywords the IR states there, as what an
unevaluated keyword applies to depends on what stands beside it; read so, the entry means what the
source means, nothing where the keyword is inert, and needs no marker or reason of another kind to
tell the two apart. Read as a schema of its own it means something else:
`{"additionalProperties": false, "unevaluatedProperties": false}` alone refuses a property the
position's `properties` declare, and a live `{"unevaluatedProperties": false}` alone refuses every
property. The keyword the entry repeats beside an inert one is context: the IR holds it already,
typed where the node has a home for it and kept verbatim where it has none
([`shape-applicator`](#shape-applicator)), and read where it was written it repeats a keyword
already there.
`prefixItems`, `contains`, `properties` and `patternProperties` evaluate only what they match and
leave the keyword live, and an entry holding only unevaluated keywords holds live ones. One an
`allOf` branch writes stays with the branch kept whole, whatever its form
([`allof-inline-residue`](#allof-inline-residue)), since at the position it would exempt what the
other conjuncts evaluate.
A keyword whose value is the empty collection JSON Schema equates with its absence states nothing,
and is read as the keyword left out, as [`null-keywords`](#null-keywords) reads a `null`: nothing
is typed, kept or reported for it, and the schema lowers as it does with the key removed.
`dependentSchemas`, `dependentRequired` and a draft's `dependencies` are read so when written as a
mapping with no entry, at every position, an inline `allOf` branch writing only one included, which
says nothing for the position and is no conjunct another branch's keyword disagrees with. Where
the dialect defines no `dependencies`, an empty one is an unknown keyword like any other
([`unknown-keys`](#unknown-keys)), as `definitions` is, and kept whatever it holds. A keyword
holding a schema, `not`, `if`, `then`, `else`, `propertyNames`, `contains` and the unevaluated
keywords, states something when it holds `{}`, the schema every instance satisfies, and is kept.
Every dialect reads both keywords as 2020-12 does. In a schema read in JSON Schema draft
4, 6 or 7 ([`dialect-keywords`](#dialect-keywords)), `dependencies`, which 2019-09 split into
`dependentSchemas` and `dependentRequired`, is kept the same way under `openapi:dependencies`, and
elsewhere is an unknown keyword ([`unknown-keys`](#unknown-keys)). Each entry is reported as an
info `openapi/validation-only-keyword` at the schema that wrote it, an `openapi:unevaluated` one
holding an inert keyword naming it and the keyword that leaves it inert. A position a kept keyword's
`$ref` lands on is lowered all the same, and so is each position a chain of them passes, an alias
keeping what it writes beside its `$ref`: the compiler lowers every position a schema `$ref` of the
document lands on, hop by hop, as it would one a lowered keyword names.

- Cases: `negation-not`, `dependent-required`, `schema-drafts`, `maps`, `tuples-prefixitems`,
  `unevaluated-at-every-outcome`, `kept-ref-landings`
- Codes: `openapi/validation-only-keyword`
- Tests: `annotation.TestValidationOnlyAt_KeepsEveryKeywordItClaims`,
  `e2e.TestOlderDrafts_DependenciesAreValidationOnly`,
  `schema.TestLower_ValidationOnlyKeywords`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`,
  `annotation.TestUnevaluatedRaw_KeepsAnInertKeywordBesideWhatLeavesItInert`,
  `annotation.TestValidationOnlyAt_AnInertUnevaluatedKeywordNamesWhatLeavesItInert`,
  `annotation.TestValidationOnlyAt_AttributesAnInertEntryByItsUnevaluatedKeyword`,
  `schema.TestClosureTyped_EveryOutcomeKeepsWhatItDoesNotType`,
  `schema.TestClosureTyped_ReadsAlikeInEitherBranchOrder`,
  `schema.TestVoices_AnUnevaluatedKeywordStaysWithItsBranch`,
  `schema.TestUse_ADraftsTupleRestLeavesUnevaluatedItemsInert`,
  `annotation.TestWritesEmptyDependencyMap_TellsAnEmptyMapFromAnEntry`,
  `statement.TestKey_DefinedTellsAKeywordTheRowsDefine`,
  `e2e.TestEmptyDependencyMap_IsTheKeywordLeftOut`,
  `e2e.TestEmptyDependencyMap_AnEntryWritingOnlyOneSaysNothing`

### `dynamic-ref`

A dynamic reference is resolved per reference site by compiler expansion, to a reference to the type
it names, reported as an info `openapi/dynamic-ref-expanded` at the keyword, since what the source
wrote as dynamic is now a fixed target. A `$dynamicRef` is first resolved against the base of the
schema resource it is written in, as a `$ref` is (JSON Schema 2020-12 §8.2.3.2), and is dynamic only
where that lands on a fragment a `$dynamicAnchor` created, so the anchor it names dynamically is one
the resource it names declares. Anywhere else it is the `$ref` it spells: a pointer, a plain name an
`$anchor` declares, another document, a name declared nowhere in the resource it names. It then
lowers and binds as that `$ref` does, through [`references`](#references), at every position, the
lowering asking the binding table whether a schema is a reference site rather than the model
whether it writes a `$ref`. It reports nothing where it resolves and `openapi/unresolved-ref` where
it does not, worded with its own keyword, a loop through it being an `openapi/cyclic-ref`
([`reference-cycles`](#reference-cycles)); one resolving nowhere lowers the top type, keeping
nothing. A name declared more than once in the resource it
names, one of those declarations a `$dynamicAnchor`, and one a merge past
[`references`](#references)' bound may hide the declaration of, are not shown to be `$ref`s, and
are read as dynamic. What makes the expansion possible is that a
document's set of `$dynamicAnchor` declarations is fixed before evaluation: a name declared exactly
once, in the reference's own resource, is the only match any dynamic scope containing a match can
hold, so it *is* the outermost matching anchor whatever path evaluation took. The anchor may stand at
any position a `$ref` can name: a component, a `$defs` entry, a property, an `items` schema, a
response's inline schema, the component keyed `""`, or a draft's `definitions` entry, which the
reference lands on as a `$ref` naming the anchor would, so the schema there is built and validated
as one a `$ref` reaches. A reference naming the anchor by a URI that puts it in another resource,
an `$id` between the two, is irreducible.

JSON Schema 2019-09's `$recursiveRef` is read the same way in a schema whose dialect defines it
([`dialect-keywords`](#dialect-keywords)), as the reference to the unnamed anchor of a resource.
2019-09 defines it only for the value `#`, which resolves as a `$ref` of `#` does at its site: to
the root of the resource it is written in. Where that root declares no `$recursiveAnchor` of `true`,
`false` being the default, the reference reads as the `$ref` it spells and expands to the root. Where
it declares one, the target is the outermost schema of the dynamic scope declaring one too, which is
that root on every path where no other schema resource holding a declaration of
`$recursiveAnchor: true`, in any document the compile reads, reaches the root's, and the reference
expands to it then. A resource reaches each one nested directly in it and each one a reference site
in it lands in, a `$ref`, a `$dynamicRef` that is one, or a mapping value, and every resource those
reach; the walk is linear in resources and reference sites, each edge a step of the binding budget,
past which nothing is known and the reference is kept. A schema under no `$id` is in its
document's resource, whose root in an OpenAPI document is the OpenAPI Object, which holds no
schema, so its `$recursiveRef` is irreducible, as its `$ref: '#'` twin is unresolved, and the
message says that an identifier on the schema would make it the root. `$recursiveAnchor` is consumed
as a reference target, as `$dynamicAnchor` is. In a dialect defining neither keyword, each is an
unknown keyword ([`unknown-keys`](#unknown-keys)).

An expanded reference lowers to a `TypeRef` whose `Target` is the ID of the type at the position it
names, the one a `$ref` to that position resolves to: a named source component's, or the type that
position's pointer gives in its document's namespace ([identity](../ir/identity.md#across-documents)),
built once, from the declaration there, whichever reference asks for it first. No node is minted or
copied per use, so which reference or declaration lowers first decides nothing. Its `Nullable` is
what the keywords beside the reference admit, as beside a `$ref`; what its target admits is not read
yet.

A reference is irreducible — and is then kept verbatim under `openapi:$dynamicRef` or
`openapi:$recursiveRef` (`degraded_lowering`) with an info `openapi/degraded-construct` at it naming
which case it was — when a `$dynamicRef`'s name is declared more than once in its document (the
target is back under the evaluation path's control), or is declared in another document alone; when
a `$recursiveRef`'s value is not `#`, or another resource declaring `$recursiveAnchor: true`
reaches its target's; when its value is not a string at all; when a
resource boundary stands between it and its anchor as above; when no schema could be built where it
names; when expanding it would close a cycle of aliases (a `$ref` cycle is refused outright for the
same reason, and expansion must not mint one the refusal cannot see); or when it is co-declared with
a `$ref`, a `oneOf`/`anyOf`, a shape of its own, or one of `items`/`prefixItems`/`required` — an
expansion takes its target whole, so it has nowhere to put a sibling applicator.

A dynamic reference is read against the declarations of the document it is written in, by this
same rule, so one in another document expands to the type lowered where that document declares its
target, in that document's namespace, never to a source anchor of that name, unless a schema
resource of another document declaring the same name reaches the resource of that declaration, by
the reach the `$recursiveRef` above reads: that resource is then in the dynamic scope, and the
outermost one declaring the name wins, so the target depends on the evaluation path and the
reference is kept. A name one document alone declares needs no walk; each name several documents
declare costs one walk of the resources, held by the binding budget, so n such names over n
resources cost n² steps, past which nothing is known and every reference resting on one is kept.
What a reference resolved to where no position of its document places it reads no anchors, so a
dynamic reference in it is kept.

Anchors are the identifiers [`references`](#references) reads, where it reads them, so one in an
example or an extension declares nothing, and one read through a YAML alias or a merge key is a
declaration where it is placed, so an anchor reached only by an alias still makes its name a
duplicate. A resource boundary is read the same way, so a property named `$id` is none. A document
read short of the whole, by the budget or by a merge past
[`references`](#references)' bound, may hide a second declaration, and a merge past it may hide an
`$id`, so a reference resting on either is kept, since an undercount is what would turn
"irreducible" into a confident expansion; the warning that reports the bound says so. A keyword
written beside a reference that expands is read as one beside a `$ref` is
([`shape-applicator`](#shape-applicator)).

- Cases: `dynamic-ref`, `keywords-at-every-outcome`
- Codes: `openapi/dynamic-ref-expanded`, `openapi/degraded-construct`, `openapi/unresolved-ref`,
  `openapi/cyclic-ref`
- Tests: `schema.TestDynamicRef_IrreducibleIsKeptAndSaysWhy`,
  `schema.TestDynamicRef_NamingNoDynamicAnchorIsTheRefItSpells`,
  `schema.TestDynamicRef_AnAnchorOnlyAnotherDocumentDeclaresIsKept`,
  `bind.TestBind_ReadsADynamicRefAsTheRefItSpellsUnlessItNamesADynamicAnchor`,
  `bind.TestBind_ReadsADynamicRefIntoAnotherDocumentOnceItIsRead`,
  `bind.TestBind_ReadsADynamicRefAMergeMayHideTheAnchorOfAsDynamic`,
  `e2e.TestCompile_ADynamicRefNamingNoDynamicAnchorReadsAsTheRefItSpells`,
  `e2e.TestCompile_AStaticDynamicRefLowersAsItsRefTwinAtEveryPosition`,
  `e2e.TestCompile_ADynamicRefResolvingNowhereIsAnUnresolvedRef`,
  `e2e.TestCompile_ATwoMemberCycleThroughADynamicRefIsACyclicRef`,
  `e2e.TestCompile_AnAliasLoopAnExpansionClosesKeepsTheReference`,
  `bind.TestRecursiveAnchor_AResourceAnotherAnchoredOneReachesIsReached`,
  `bind.TestRecursiveAnchor_AWalkTheBudgetCutsKnowsNothing`, `bind.TestBind_TheRecursiveReachGrowsLinearly`,
  `e2e.TestCompile_ARecursiveRefNoOtherAnchoredResourceReachesExpands`,
  `e2e.TestCompile_ARecursiveRefAnotherAnchoredResourceReachesIsKept`,
  `e2e.TestCompile_ARecursiveRefAnotherDocumentsAnchoredResourceReachesIsKept`,
  `e2e.TestCompile_ARecursiveRefInAResourceNestedInAnAnchoredOneIsKept`,
  `schema.TestDynamicRef_AnAnchorAnotherDocumentsResourceReachesIsKept`,
  `bind.TestTable_ANameSeveralDocumentsDeclareIsWalkedOncePerName`,
  `bind.TestTable_ANameTheBudgetCutsTheWalkOfIsNotKnown`,
  `e2e.TestCompile_ADynamicRefAnotherDocumentsResourceReachesIsKept`,
  `e2e.TestCompile_ADynamicRefNoOtherDocumentDeclaresTheNameExpands`,
  `e2e.TestCompile_ADynamicRefNamesTooManyForTheReachWalkAreKept`,
  `schema.TestDynamicRef_ExpandsAgainstTheDocumentItIsWrittenIn`,
  `schema.TestDynamicRef_ExpandsToAnyPositionARefNames`, `schema.TestDynamicRef_ExpandsWithinOneResource`,
  `schema.TestDynamicRef_ExpandsToAnAnchorOnlyADraftPlaces`, `schema.TestResolvesAt_AnswersAsResolvePointerDoes`,
  `schema.TestRecursiveRef_ExpandsWhereItsTargetIsStatic`,
  `schema.TestRecursiveRef_IsAKeywordOnlyWhereItsDialectDefinesIt`,
  `schema.TestRecursiveRef_LandsOnTheRootOfItsResource`, `schema.TestSameResource_SaysWhyTwoPositionsMayLieInTwo`,
  `bind.TestLandDynamic_LandsWhereTheRefItSpellsWould`,
  `e2e.TestOlderDrafts_ARecursiveRefExpandsWhereItsTargetIsStatic`,
  `e2e.TestOlderDrafts_ARecursiveRefReadsAsItsDynamicTwin`,
  `e2e.TestOlderDrafts_ARecursiveRefReadsItsDocumentsAnchorsOnce`,
  `bind.TestDynamicAnchor_CountsWhatADocumentDeclares`,
  `bind.TestDynamicAnchor_SaysWhenTheCountProvesNothing`,
  `bind.TestRecursiveAnchor_CountsWhatADocumentDeclares`,
  `bind.TestResource_NamesTheRootOfAPositionsResource`
- Status: partly built: an expanded reference reads no null from its target (#1967).

### `content-vocabulary`

The content vocabulary is modelled, not preserved. All three keywords share one home, so a position
keeps them all or lowers them all: `contentEncoding` to `Encoding.Name`, `contentMediaType` to
`Encoding.MediaType`, and `contentSchema` to `Encoding.Schema`
([constraints](../ir/constraints.md)) on the scalar a `string` position lowers to, the one type the
vocabulary applies to (JSON Schema Validation 2020-12 §8.2), or a type array's `string` variant
([`type-arrays`](#type-arrays)). `contentSchema`'s value is a schema and lowers like one — hoisted
at its own source pointer and referenced by ID — rather than riding along as a payload a consumer
would have to re-parse. Where the position lowers to a shape with no `Encoding` field, or to one
whose type is no `string`, such as a number's `Scalar` hoisted for its format, all three stay
verbatim under `no_ir_home`, each under `openapi:<keyword>` and each reported as an info
`openapi/degraded-construct` at it ([boundaries](../ir/boundaries.md#validation-only-constructs)).
A `contentEncoding` on a string writing no format names what the string already is, the encoded
text, so its `Encoding` names no `WireType` ([encodings](../ir/constraints.md#encodings)).

- Cases: `content-vocabulary`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestContentVocabulary_LowersToEncoding`,
  `schema.TestContentVocabulary_KeptWhereNoEncodingHolds`,
  `schema.TestContentVocabulary_ANumberTakesNone`,
  `schema.TestLower_ATypeArraysContentIsTheStringVariants`

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
`http://json-schema.org/draft-04/schema#`, have keyword rows of their own, and a draft the rules it
states of them, such as draft 4's `required` holding an entry, beside the Schema Object's. A dialect
changes the keywords alone: the fields OpenAPI adds to a Schema Object, such as `discriminator` and
`xml`, and the rules it states of one stay the minor's. A reading the rows decide follows the
schema's: draft 4
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
an alias places under two roots is read in each one's at each, by the folding of an `allOf`, the
reading of a body as a file and the rule table's check too. A schema only a reference reaches, where
no grammar reads one, an extension's content or raw YAML a document of another kind holds, is a root
too where it writes a `$schema` naming a dialect and no other such schema holds it, which the set
of them decides and no order of reading does; what it holds is read in its dialect from there, and
no identifier in it declares anything ([`references`](#references)).

A keyword a schema writes meets one of four outcomes, each taken from the rows of the dialect the
schema is read in, never from its version or its name alone:

1. The rows define it: it is read as its row says, and a value the row refuses is the rule table's
   finding ([`validation`](#validation)), an error for a wrong shape.
2. The rows do not define it and the dialect the compiler reads natively does, JSON Schema 2020-12,
   or for a 3.0 schema 3.1's rows: it is read as that dialect reads it, and reported once at its key
   as an `openapi/later-version-construct` warning
   ([`later-version-construct`](#later-version-construct)), which names the three such keywords
   left unread.
3. Only an earlier draft defines it, such as `definitions`, `dependencies`, `additionalItems`, `id`,
   `$recursiveRef` or `$recursiveAnchor` outside the drafts defining them, or no dialect does: it is
   kept verbatim as [`unknown-keys`](#unknown-keys) keeps any keyword the schema model does not name,
   with no native reading to apply. Guessing which draft an author meant would be a heuristic, where the document
   has a declared way to ask for it, naming the draft by `$schema` or `jsonSchemaDialect`, and JSON
   Schema has an unknown keyword ignored (2019-09 §4.3.1) or read as an annotation (2020-12
   §4.3.1). A JSON pointer `$ref` into such a keyword's value still resolves, read as a schema.
4. The rows define it and give it no effect where it is written: it is read by its declared effect,
   and warned of, as a 3.0 `nullable` with no `type` beside it is ([`nullable`](#nullable)), and a
   keyword beside a `$ref` that a draft ignores ([`later-version-construct`](#later-version-construct)).

The second outcome and the third differ on purpose: the later reading loses least, since a consumer
can set a warned keyword aside but cannot rebuild a lowering from a verbatim copy.

`$comment` is dropped outright — 2020-12 §8.3 forbids presenting it to end users, so no emitter may
see it. `$dynamicAnchor` is consumed as a reference target rather than carried as content — it is
what lets a `$dynamicRef` expand ([`dynamic-ref`](#dynamic-ref)), and a `$ref` naming it reads it as
an `$anchor` — and so is 2019-09's `$recursiveAnchor`, where the schema's dialect defines it.
`$anchor` is consumed the same way: a `$ref` naming one that [`references`](#references) reads
resolves to the schema declaring it.

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
  `load.TestReadDialects_GivesAnotherDocumentsOwnRowsToWhatNoRootHolds`,
  `e2e.TestCompile_ASchemaOnlyARefReachesReadsItsOwnDialect`,
  `e2e.TestCompile_ASchemaAnotherLandingHoldsIsNotADialectRoot`

### `vocabulary-coverage`

Every keyword of the JSON Schema 2020-12 vocabularies reaches the compiled IR, lowered by a rule of
this document or kept verbatim, its presence and its value both, with four exceptions that change
nothing by design: `$comment`, which the specification forbids surfacing; `$defs`, whose members
lower where a `$ref` reaches them; and `$dynamicAnchor` and `$anchor`, each consumed as a reference
target rather than carried as content ([`dialect-keywords`](#dialect-keywords)). The keyword lists
of [boundaries](../ir/boundaries.md#validation-only-constructs) and of this section are held to that
rather than merely asserted: `TestCases_CoverTheRuleTable` holds the schema cases to every keyword
of the rule table at each version, and `openapicases.TestCore_EveryKeywordIsCarriedEverywhere`
compiles, for each keyword at each position the cases write a schema and in each spelling they write
it in (merged into the schema it is said of, beside a `$ref` to that schema, beside an `allOf` of
the `$ref`, on an entry of the `allOf` after the `$ref`, and on such an entry nested one `allOf`
deeper), three documents: the keyword absent, written with one value, written with a second.
It requires the absent form to differ from the first and the first from the second, and an exception
to equal the absent form, its justification written beside the keyword; where the target writes the
keyword itself, there is no absent form, and the two values are compared alone. So are the values of
`$ref` and `$dynamicRef` in effect: each names components the absent form does not write, and
differs from it by them, so one dropped is found as a value discarded. A keyword that stops
being carried at any one position or in any one spelling fails there rather than waiting for a
reader to notice, and a finding that stands is listed with its reason in the package's
`testdata/presence.txt`. The test asks this of the core cases, which the gate runs;
`morphic-harness -cases full` asks it of every case, an entry written before the `$ref` among them,
and reports what the record does not explain. The guarantee is a floor rather than an inventory:
it compares one pair of values per keyword, so a lowering that carried one value and dropped
another would still pass. A dependency map with no entry is no value in that sense but the keyword
left out ([`validation-only`](#validation-only)). A keyword kept verbatim keeps its `$ref`s as written, and the positions
they land on are lowered ([`validation-only`](#validation-only)).

- Tests: `e2e.TestCases_CoverTheRuleTable`, `openapicases.TestCore_EveryKeywordIsCarriedEverywhere`,
  `schema.TestPromiseLandings_BuildsNothingTwice`, `schema.TestPromiseLandings_LowersWhatNothingElseReached`,
  `e2e.TestCompile_KeptRefLandingsAreLoweredOnceInEitherOrder`
- Status: partly built: a `$dynamicRef` written beside a `$ref` at a position no property,
  parameter, header or component holds reaches the IR in no form (#2009).

## 8. Paths and operations

### `path-keys`

A Paths Object key lowers to `HTTPBinding.URITemplate` as written, less a fragment: from the first
`#` outside a template expression, or the first after a `?`, the key names a fragment, which no
request sends (RFC 3986 §3.5). The template is the key before it, and each operation the path
mounts keeps the fragment, the text after the `#`, on its HTTP binding as `openapi:fragment`, of
reason `degraded_lowering`, at the key's pointer. The operation's ID and the name hint of one with
no `operationId` read the whole key, so operations a document declares on one route under keys
that differ only in a fragment keep names of their own, while grouping by path prefix
([`path-prefix-groups`](#path-prefix-groups)) reads the template. A path operation whose method and
template, each expression's name aside, another path operation sends too has
`HTTPBinding.SharedRoute` set; a webhook's and a callback's operation are sent by the API to no
route a client sends. A key that breaks OpenAPI's path templating rules,
those of 3.2.0 §4.8.2 read at every version — no leading `/`, a character a path admits only
percent-encoded, a brace outside a template expression, an expression used twice, an empty segment,
a query, or a fragment — lowers as declared but for the fragment, and is reported as one
`openapi/invalid-path-key` warning at its path item, naming each rule the key breaks and nothing in
a fragment. That every template expression has a path parameter, in the path item or in each of its
operations, that every path parameter names an expression of the key less its fragment, and that no
two templated paths differ only in their names is [`validation`](#validation)'s to report, as an
error.

- Cases: `http-binding`, `path-key-fragments`
- Codes: `openapi/invalid-path-key`
- Tests: `operation.TestPathKeys_MalformedKeyIsReportedOnce`,
  `operation.TestPathKeys_RulesAreNotVersionGated`,
  `operation.TestPathKeys_FragmentIsLeftOutOfTheTemplate`, `operation.TestPathKeyDefects`,
  `operation.TestPathRoute_SplitsAsTheRuleTableDoes`,
  `operation.TestSharedRoute_MarksEveryOperationSendingARouteAnotherSends`,
  `operation.TestRouteShape_LeavesOutEachExpressionsName`,
  `oas.TestCheck_PathTemplatesAnswerToPathParameters`, `oas.TestFragmentAt_SplitsAsTheLoweringDoes`

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
that neither declares names none, and its service's apply ([servers](../ir/servers.md)), save to a
webhook's or a callback's operation, which inherits none
([servers](../ir/servers.md#inbound-operations)). An explicitly empty list, `servers: []`, declares
none, as an absent one does, and so overrides nothing above it: OpenAPI equates an empty list with
an absent one where it says what an empty list means, the document's, and an operation reading `[]`
as no server would have none to be sent to, so nothing is kept or reported, as for the document's.
A path item's list is registered even where every one of its operations overrides it. Two lists are
not: one a nearer layer of a mount overrides, which [`path-item-ref-fields`](#path-item-ref-fields)
reports rather than lowers, and one written by a component path item no reference reaches, kept
whole with that component ([`unreferenced-components`](#unreferenced-components)). A path item that
mounts no operation registers its servers too, named by nothing, and keeps what else it writes on
the service instead, or on the declaring operation's HTTP binding for a callback expression: its
`x-*` names and undeclared keys under `openapi:pathItem/<item pointer>/<key>`, its summary and
description as [`path-item-docs`](#path-item-docs) says, and its parameters as
[`parameters`](#parameters) says. The item's pointer is in every key, since one carrier holds every
such item, and a component item two paths mount is kept once per mount. It is reported as one
`openapi/degraded-construct` warning at the mount's use site, in the document writing it, saying
what any of its layers kept: the mount for a path or a webhook, and the expression's key in its
Callback Object for a callback expression, whose mount a `$ref`'d callback or parent addresses
nowhere.

- Cases: `webhooks`, `callbacks`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperations_PathItemServersNamedOnEveryRoute`,
  `operation.TestOperations_OwnServersOverrideThePathItems`,
  `operation.TestOperations_ServersEachLevelAlone`,
  `operation.TestOperations_AnEmptyServersListOverridesNothing`,
  `operation.TestOperations_InboundOperationsNameOnlyTheServersTheyState`,
  `operation.TestPathItem_WithNoOperationIsReportedAtItsUseSite`,
  `operation.TestPathItemRefSiblings_UnmountedSaysOnceWhatEveryLayerKept`,
  `servers.TestRegistry_DeclaredOneDeclarationLowersOnce`,
  `operation.TestPathItem_WithNoOperationKeepsWhatItWroteOnTheService`,
  `operation.TestPathItem_WithNoOperationKeepsEachMountApart`

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
A pointer through a path item `$ref` reads by the mount's rule: a token naming a field written
beside the `$ref` reads that field, at its own pointer, the layer the mount keeps, and any other
reads on in the item the `$ref`'s chain ends at, so a pointer and a mount read one mapping alike
whichever reference, schema `$ref` or reference object, writes it. A `#/$defs/...` pointer written
in those fields reads its definition there by the same rule. An item another document holds is
declared where that document declares it, and a finding names its positions by the reference. The
fields beside a `$ref` are lowered at every position the bindings' table lands a path item on, in
the source or another document and under an `x-*` key alike, declared where they are written.

- Cases: `path-item-ref-siblings`
- Codes: `openapi/path-item-ref-collision`
- Tests: `operation.TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins`,
  `operation.TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefReadsThem`,
  `operation.TestPathItemRefSiblings_APointerIntoTheFieldsTakesWhatTheyDeclare`,
  `operation.TestPathItemRefSiblings_APointerBesideTheRefReadsTheFieldsInEitherOrder`,
  `operation.TestPathItemRefSiblings_ADefsPointerBesideARefReadsItsOwnDefinitions`,
  `e2e.TestReferenceObjects_AFieldBesideAPathItemRefIsReadWhereItIsWritten`,
  `operation.TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreModelled`,
  `operation.TestPathItemRefSiblings_FieldsAtAnyLandingAreModelled`,
  `operation.TestPathItemRefSiblings_AForeignReferentKeepsItsOwnPositions`,
  `e2e.TestCompile_FieldsBesideAPathItemRefAtAnyLandingAreMounted`,
  `e2e.TestCompile_ALongChainOfLandingsMountsEachOnesFields`,
  `e2e.TestCompile_FieldsBesideAForeignPathItemRefAreMounted`,
  `e2e.TestExternalContent_FieldsBesideAPathItemRefItWritesAreLowered`

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

A webhook's operation is filed in the synthesized `webhooks` group ([`webhooks`](#webhooks)) and a
callback's in the group of the operation declaring it ([`callbacks`](#callbacks)), whatever tags
either states, under this strategy and every other: each is a request the API sends, filed by the
route mounting it, and `Operation.Tags` keeps its tags. Filing a callback by its own tags was
weighed and rejected: one stating none, as callback operations commonly do, would land in the
untagged group, apart from the operation whose exchange supplies its URL.

An operation's `tags` are the groups it is a member of, so `Operation.Tags` keeps a tag the list
writes again once, at its first occurrence, comparing tags as the text written, and the repeat is
reported as an `openapi/repeated-entry` warning where it is written, naming the first. A blank tag
is a tag like any other.


A tag object's own keys are kept on its `TagDef`, its vendor extensions and undeclared keys unscoped
and its `externalDocs` object's under `openapi:externalDocs/`. A group a declared tag forms takes the
vendor extensions of the tag's first declaration in its own `Unmodeled`, as it takes its docs, and a
stability tier they state fills the group's `Stability`, recorded in its `Inferred`
([`extension-promotion`](#extension-promotion)). The tier is read through the group and nowhere
else: an operation the tag files states none, and a mapped key on a tag that forms no group fills
nothing and is reported as an info `openapi/unpromoted-extension` at the key. A deprecation a tag's
keys state, and each member they write, fill nothing either, a group holding no `Deprecation`, so
each key read into one is reported the same way, once, whether the tag forms a group or not.

- Cases: `tags-grouping`, `tags-grouping-32`, `group-identity`, `repeated-tags-and-scopes`
- Codes: `openapi/repeated-entry`, `openapi/unpromoted-extension`
- Tests: `operation.TestGrouping_ACallbackGoesWithItsParentWhateverItsTags`, `operation.TestGroups_ARepeatedTagNameTakesItsFirstDeclaration`,
  `operation.TestGroups_ADeprecationOnATagIsReported`, `promotion.TestUnheldDeprecation_ReportsEachKeyStatingOne`, `e2e.TestCarriedKeys_ATagKeepsItsKeysAndItsGroupTakesTheTier`, `e2e.TestTags_KeysOnTheTagAndTierOnItsGroupEndToEnd`,
  `operation.TestGrouping_ParentCycleNestsNoMemberUnderAnother`,
  `operation.TestGrouping_DanglingParentStaysTopLevel`,
  `operation.TestGrouping_SameNameDeclaredTwiceIsDeterministic`,
  `operation.TestGrouping_BadgeOnlyOperationFallsToDefault`, `oas.TestCheck_TagParentsExist`,
  `oas.TestCheck_DuplicatesAreFound`, `operation.TestOperation_ARepeatedTagIsKeptOnce`,
  `operation.TestOperation_ALongTagListFoldsAsAShortOne`

### `path-prefix-groups`

Under the `path-prefix` grouping strategy (`Options.Grouping`), each path operation is filed in one
group, named by the first non-empty segment of its path's URI template, the key less any fragment
and any query ([`path-keys`](#path-keys)), so `/users`, `/users/{id}` and `/users?role=admin` share
`users`, and `/?probe=1` files under the empty segment. The query is found as
[`path-keys`](#path-keys) finds it, so a `?` inside a template expression begins none, and
`/{a?b}/users` files under `{a?b}`; `URITemplate` keeps the query. A segment that is a
template expression names its group as written, `/{tenant}/users` filing under `{tenant}`, and the
root path, whose segments are all empty, under the empty one. The group's ID is `g/path-prefix/`
and the segment as one RFC 6901 token, the empty segment's `g/path-prefix`
([identity](../ir/identity.md#operation-groups)); it is named by the segment, by a hint where the
segment is empty, and has no docs, and group order is first-seen from the operations. A path names
no resource, so the reading is policy (invariant 6), never the default: an operation it files lists
`group-path-prefix` in its `Provenance.Inferred`. A webhook's operation stays in the webhook group,
unmarked, and a callback's goes with the operation declaring it, marked as that one is
([`tag-groups`](#tag-groups)). Nothing is reported.

- Cases: `path-prefix-query`
- Tests: `operation.TestGrouping_PathPrefixGroupsAreIdentifiedByTheirSegment`,
  `operation.TestGrouping_ByPathPrefixInferred`, `operation.TestGrouping_PathPrefixRootPath`,
  `operation.TestSharedRoute_PathPrefixGroupsTheRoute`,
  `operation.TestGrouping_ACallbackGoesWithItsParentWhateverItsTags`,
  `operation.TestGroups_PathPrefixStopsAtTheQuery`,
  `operation.TestGroups_PathPrefixKeepsTheQueryInTheTemplate`,
  `operation.TestGroups_PathPrefixReadsAQuestionMarkInsideATemplateExpression`

### `extension-groups`

Under the `extension` grouping strategy (`Options.Grouping`), an operation's groups are read from a
vendor extension stating a *group path*, the nested sub-client an SDK generator files it under: a
group for each segment, each nested in the group its prefix names. OpenAPI gives an `x-` key no
meaning, so the reading is policy (invariant 6), never the default: an operation it files lists
`group-extension` in its `Provenance.Inferred`, and the extension stays in `Unmodeled` untouched
([`vendor-extensions`](#vendor-extensions)). `Options.GroupingExtension` names the key, by default
`x-fern-sdk-group-name`, read on the operation alone, where the conventions write it: a key its path
item writes is not read for the item's operations. A string is one path, its segments separated by
dots, so `zero-trust.devices.policies` nests three groups. Conventions disagree on a list: one
writes the segments of a single path, as `x-fern-sdk-group-name` does, another several paths, each a
group the operation belongs to, as `x-speakeasy-group` does. So a list's reading is named beside the
key, `segments` or `groups`, and is never taken from another key's: a key named without one is
refused, as is one without the key. Under `segments` each entry adds its own segments; under
`groups` the operation is filed under the first entry's path.

An operation can belong to further groups than the one it is filed in: each path after the first a
`groups` list names, and each an *alias* names. `GroupingExtension.Aliases` names the extension
listing an operation's aliases, by default `x-forge-aliases` beside the default key, and none beside
a key the caller names unless the caller names one: a list of objects, each stating a path under the
grouping key as that key's own value does, read under its list reading, so the default's entries
restate `x-fern-sdk-group-name` beside a method name of their own. An object stating no path names
none. Each such path gets its groups too, nested as the operation's own are and holding no
operation where none is filed there, so that a remount, which names a group by its ID
([shaping hints](../emitters/contract/shaping-hints.md)), names one the IR holds; placing the
operation there is the remount's, read from the kept extension, since an `Operation` sits in one
group. Nothing marks such a group `Inferred`: an `OperationGroup` has no provenance, and the
marker is on each operation the extension files, which a group holding none lacks. Leaving those
groups to the remount was weighed and rejected: an ID is opaque to a consumer
([identity](../ir/identity.md)), which could then neither name nor nest a group the IR does not hold.

So the aliases extension has two readers, which must name the same key: `GroupingExtension.Aliases`
(`--opt grouping-aliases`), for the groups an alias names, and the key an embedded-policy mapping
reads into an operation's `AlsoMount` ([embedded policy](../emitters/contract/embedded-policy.md)),
for the mounts in them, which the mapping names as `Unmodeled` holds it, `openapi:x-forge-aliases`
for the default. A mapping has no default key, so the two pair only where its configuration names
the key this option reads. A mapping reading another key can name a group the IR does not hold, a
warning `emitters/unmatched-key` that mounts nothing ([a key that matches
nothing](../emitters/write-integrate.md#a-key-that-matches-nothing)), and a group made for a key no
mapping reads is named by no mount.

A group's ID is `g/extension/` and the path's segments, each an RFC 6901 token
([identity](../ir/identity.md#operation-groups)). It follows from the path alone and never from the
key, so a configuration can name a group, and a group nests in the one its prefix names whichever
operation is met first. A group is named by its segment, and has no docs, as no Tag Object describes
it; group and sibling order are first-seen from the operations, as under tags, an operation's own
groups before those its other paths name.

An operation whose key is absent, or holds empty text, an empty list or null, is grouped by
`GroupingExtension.Fallback`, tags by default or path prefix, and marked as that strategy marks it;
its aliases still name groups. So is one whose value names no path: one that is neither text nor a
list of text, or whose path has an empty segment (`a..b`, `.a`, a list holding `""`) or more than 16
segments. A list is judged whole, so under `groups` an entry after the first that names no path is
enough. That bound is over twice the deepest path the golden spec writes, and caps what a path's
groups weigh: each group's ID spells the path to it, so their IDs weigh half the path's depth times
its length. An aliases list names no path where it is anything but a list of objects, null or
empty, or where a path one of its objects states names none, and is judged whole too, so it names
no group. Each value naming no path is reported as an info `openapi/degraded-construct` at its key
in the operation's declaration, in whichever document declares it, so a path item two paths `$ref`
is reported once. A value that does not serialize is reported where any extension's is
([`vendor-extensions`](#vendor-extensions)), and not again. A webhook's operation stays in the
webhook group and a callback's in its parent's, whatever its keys state ([`tag-groups`](#tag-groups)),
and neither's keys name a group.

- Codes: `openapi/degraded-construct`
- Tests: `operation.TestGrouping_ByExtensionNestsAGroupPerSegment`,
  `operation.TestGrouping_ByExtensionReportsAValueStatingNoPath`,
  `operation.TestGrouping_ByExtensionFallsBackToPathPrefix`,
  `operation.TestGrouping_ByExtensionListOfGroupsMintsAGroupForEach`,
  `operation.TestGrouping_ByExtensionMintsAGroupForEachAlias`,
  `operation.TestGrouping_ByExtensionRemountsAreIndependentOfDeclarationOrder`,
  `operation.TestGrouping_ByExtensionReadsAReferencedPathItemWhereItIsDeclared`,
  `operation.TestGrouping_ByExtensionTreeIsIndependentOfDeclarationOrder`,
  `service.TestGroupingExtension_PathsReadEachSpelling`,
  `service.TestGroupingExtension_AliasPathsReadEachSpelling`, `e2e.TestGolden_ExtensionGroups`,
  `e2e.TestGroupingByExtension_ReportsInTheDocumentDeclaringTheOperation`,
  `openapi.TestCompile_GroupingExtensionDefaultsAndRefusals`,
  `openapi.TestCompile_GroupingAliasesFollowTheKeysTheyRestate`

## 9. Parameters

### `parameters`

An operation's own parameters, in order, and then those of its path item it does not override, in
order, lower to the operation's `Params`, an operation's parameter overriding a path item's of the
same `name` and `in`, a header's `name` compared in any case, as HTTP compares a field's name
(RFC 9110 §5.1), so `x-trace` overrides `X-Trace`, and so does a nearer layer's of a path item a
`$ref` composes. Each is placed on the HTTP binding at its location (`path`, `query`,
`querystring`, `header` or `cookie`) under its wire name, with its `style`, `explode` and
`allowReserved` ([http-binding](../ir/http-binding.md)). The `style` is the location's where the
parameter writes none, and the `explode` is as declared, left unset where the parameter writes none,
since its default depends on the style and filling it in would present the default as a
declaration. A parameter's ID is derived from the operation's mount pointer, its name and its
location ([identity](../ir/identity.md)). A list
repeating an earlier entry's `name` and `in`, its `$ref`s resolved and a header's `name` compared
in any case, which OpenAPI forbids, keeps every occurrence, the ID of each repeat of one spelling
ending in its occurrence number, so `x-trace` beside `X-Trace` keeps the ID each has alone, and is
reported as an `openapi/duplicate-parameter` error at the repeat, naming the first entry's spelling
where it differs. Every list the source writes is
checked, whether or not it lowers, a path item's a reference lands on where the source's grammar
reads none, such as under an `x-` key, among them, beside its `$ref` or not, once however many paths
mount it, and a list another document writes where it lowers or where a path item that mounts no
operation keeps it; the rule table
([`validation`](#validation)) states this rule and leaves it to this one. A path item that mounts
no operation has none to merge its parameters into, so it keeps each entry as written, a `$ref`
included, where it keeps what else it writes ([`path-item-servers`](#path-item-servers)), under
`openapi:pathItem/<item pointer>/parameters/<name>/<in>`, the path its ID would be derived from, a
repeat's ending in its occurrence number (`no_ir_home`).

Each parameter list and servers list is checked once, where it is written, in whichever document:
the source's, and those of each path item and its operations a `$ref` reaches, raw YAML of the
source or a node of another document, each placed in that document's own entry however many
references reach it.

- Cases: `param-styles`, `param-style-matrix`, `param-identity`, `header-name-case`,
  `header-name-syntax`
- Codes: `openapi/duplicate-parameter`
- Tests: `operation.TestParams_ExplodeLowersAsDeclared`,
  `operation.TestParams_RepeatedNameAndLocationKeepsEveryParameter`,
  `operation.TestPathItem_WithNoOperationKeepsItsParameters`,
  `operation.TestPathItem_WithNoOperationNumbersARepeatedParameter`,
  `operation.TestPathItem_WithNoOperationReportsARepeatInAnotherDocument`,
  `operation.TestParams_AHeaderIsOneParameterInAnyCase`,
  `operation.TestPathItem_WithNoOperationKeysAHeaderRepeatedInAnyCase`,
  `ids.TestSlotOf_ComparesAHeadersNameInAnyCase`,
  `e2e.TestCompile_AParameterListRepeatingAnEntryIsReportedWhereverItIsWritten`,
  `e2e.TestCompile_ARepeatInAListTwoReferencesReachIsReportedOnce`,
  `e2e.TestCompile_AListAtAPathItemLandingIsCheckedForRepeats`,
  `e2e.TestCompile_AListAtAPathItemLandingInAnotherDocumentIsCheckedForRepeats`

### `content-parameters`

A parameter or header that states its type with `content` instead of `schema` lowers through one
media-type entry, which OpenAPI requires to be the only one: the lowest key as written where a
document declares more, so that no declaration order changes the type. Its schema is the type,
lowered as the `schema` spelling's is with the same constraints and `xml` hints, hoisted at
`<owner>/content/<key>/schema`, or the top type where the entry writes none, and the key as written
is the media type that serializes the value: `HTTPParamBinding.ContentType` on a parameter,
`Property.Encoding.MediaType` on a header, neither set by the `schema` spelling; a parameter's own
`Parameter.Encoding` is written only by [`http-date-inference`](#http-date-inference). Every other entry
lowers nothing and is kept whole on the Document under `openapi:<coordinate>`, its own pointer behind
a document's qualifier where it is not the source (`degraded_lowering`), once however many references
reach it: such an entry belongs to the declaration, which no node stands for, and a parameter or
header lowers once per reference. Each is reported in one `openapi/degraded-construct` warning at the
`content` map, beside the `openapi/validation` error ([`validation`](#validation)) such a map draws,
which names the lowest `annotation.MaxUnknownKeys` and counts the rest. The media type serializes
the value, so a parameter whose content entry is elected states no style: its binding's `Style` is
the one it writes, and empty where it writes none, where the `schema` spelling takes its
location's default. A `style`, `explode` or `allowReserved` written beside `content`, which OpenAPI
applies to a value serialized through its schema (3.0.4: "For simpler scenarios, a `schema` and
`style` can describe the structure and syntax of the parameter"), has no effect there: it lowers as
declared and is an `openapi/inapplicable-location-keyword` warning at the key, a header's `style`
and `explode` alike ([`header-style`](#header-style)), and `in: querystring`'s are
[`querystring`](#querystring)'s alone, as 3.2 bars the three there. Where a location's own row also
holds, as for `allowReserved` at `in: path`, both are reported, each a different statement.
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
- Codes: `openapi/degraded-construct`, `openapi/inapplicable-location-keyword`
- Tests: `oas.TestCheck_SerializationKeywordsBesideContent`,
  `operation.TestParameter_ContentStatesNoDefaultStyle`,
  `e2e.TestContentParameters_SerializationKeywordsAreReported`,
  `operation.TestParams_ContentStyleParameter`,
  `operation.TestPassedOverEntries_ReportsExtraMediaTypes`,
  `operation.TestPassedOverEntries_KeepsEveryEntryAndNamesTheLowest`,
  `operation.TestPassedOverEntries_KeepsOneCopyPerDeclaration`,
  `operation.TestElectContentEntry_DoesNotFollowDeclarationOrder`,
  `operation.TestParamContent_OwnAndMediaExamplesAreBothKept`
- Status: partly built: what a referenced parameter or header keeps is copied per reference (#1240).

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
`openapi/reserved-header-name` warning at its `name`: dropping declared content is an emitter's
call, not a compiler's, and the diagnostic lets an emitter suppress it. A `headers` map of a
Response Object or an Encoding Object reserves `Content-Type`, which each states elsewhere, and an
entry under it is reported the same way at the entry. Names compare in any case, as HTTP compares
field names. The rule table reports both ([`validation`](#validation)), wherever the object is
written, so a component nothing references is reported, and one many operations use is reported
once.

- Codes: `openapi/reserved-header-name`
- Tests: `operation.TestParams_ReservedHeaderNamesAreReported`,
  `oas.TestCheck_ReservedHeaderParameter`, `oas.TestCheck_ReservedHeaderEntries`,
  `e2e.TestReservedHeaderNames_ReportedOnceWhereverWritten`, `e2e.TestUnknownKeys_ObjectsKeptWhole`

### `idempotency-inference`

The idempotency policy, `Options.IdempotencyInference` and the text option `idempotency-inference`
([`policies`](#policies)), reads an operation's idempotency token off the header its key travels
in. Its trigger is a header parameter named `Idempotency-Key`, compared without case, the field the
IETF HTTPAPI working group's Idempotency-Key draft defines for a caller's key; no other spelling is
read, as no other header carries a key on the golden spec or in 1,840 public OpenAPI 3 documents,
and the one other parameter the golden spec names for one, the query parameter `idemp_key` of
`POST /accounts/{account_id}/bulk/subscriptions`, abbreviates the name and describes nothing. An
operation bound by `POST`, `PUT`, `PATCH` or `DELETE`, the methods among OpenAPI's fixed fields that
RFC 9110 does not define as safe (§9.2.1), that states no idempotency of its own and binds exactly
one such header takes an `Idempotency` of the `idempotency_token` kind whose `TokenParam` names that
parameter ([operations](../ir/operations.md)), with no report, as the document leaves the field
silent. An operation a webhook or a callback declares takes its token too: the API sends that
request, and the key tells its receiver a delivery it has seen.

A safe method needs no key, and its binding already states what a token would, the plan reading the
operation as safe off it, so the document has spoken: `GET`, `HEAD`, `OPTIONS`, `TRACE` and `QUERY`
take no token and draw no report. Two cases meet the trigger and decline it, each reported as an
info `openapi/undecided-idempotency` at the operation, saying why, the operation keeping the
idempotency it states, which is none: a method only an `additionalOperations` entry declares, whose
safety the compiler does not know, and two header parameters of that name, which can differ only in
case, a repeat the compiler reports as header names compare in any case ([`parameters`](#parameters)),
and so name no one token. On the golden spec it names the token of the 6 operations declaring
the header, 5 `POST` and a `PUT`, and reports none: 4 describe it as what makes a retry safe or keeps
a request from applying twice, and 2 as a client command key whose reuse for another request is
refused with a `409`, as a reused idempotency key is (spec @86556f6f).

- Cases: `idempotency-inference`
- Codes: `openapi/undecided-idempotency`
- Tests: `convention.TestIdempotency_TheKeyHeaderIsTheToken`,
  `convention.TestIdempotency_ReportsWhatItCannotSettle`,
  `convention.TestIdempotency_DisabledReadsNoToken`,
  `openapi.TestDecodeOptions_TextReachesEveryField`,
  `e2e.TestConventions_DeclarationOrderChangesNothing`, `e2e.TestConventions_DisabledFillsNothing`

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
`ir.Parameter` has no field for either. Everything a parameter's or a header's schema writes that the
carrier keeps is keyed beneath the schema's scope, `openapi:schema/<key>`, or
`openapi:content/<mt>/schema/<key>` for the schema of an elected content entry, and the object's own
keys stay unscoped, so a parameter and its schema can each write one key and both are kept
([unmodeled](../ir/unmodeled.md#keys)). A model's property is its schema, and keeps its keys
unscoped. An entry another entry of the carrier holds whole is kept once: an `xml` object kept whole
holds its own extensions, and an `allOf` branch kept whole holds a `readOnly` read out of it, so
neither inner key is kept again, compared by provenance and value, never by the key's spelling;
a discriminator kept whole on a type holds its extensions the same way. A key the promotion policy
reads is the exception, kept beside what holds it at any scope: a promotion reads the kept entries,
and one read only inside another's value would fill no field, so a branch kept whole that writes
`x-sensitive` still redacts its carrier ([unmodeled](../ir/unmodeled.md#promotion)). The `xml` of
a schema that owns a node of its own stays on that node's `XMLHints` instead. A schema with no node
of its own keeps there too what any carrier keeps: the content vocabulary ([`content-vocabulary`](#content-vocabulary)), a `$dynamicRef` that
did not expand ([`dynamic-ref`](#dynamic-ref)) and a keyword nothing reads
([`unknown-keys`](#unknown-keys)).

- Cases: `param-xml-residue`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_SchemaVisibilityKeptUnderUnmodeled`,
  `operation.TestParams_KeepWhatAnyCarrierKeeps`,
  `operation.TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode`,
  `operation.TestParams_SchemaXMLHintsStayOnAnOwnedNode`,
  `operation.TestParameter_SchemaAndObjectSpellOneKey`, `e2e.TestUnknownKeys_ACarrierAndItsSchemaKeepOneKeyApart`,
  `annotation.TestUnder_KeysEachEntryBeneathTheScope`, `promotion.TestDropContained_KeepsWhatIsNotInside`,
  `promotion.TestDropContained_KeepsAKeyAPromotionReads`, `operation.TestPromotion_ReadsAKeyInABranchKeptWhole`,
  `operation.TestParameter_XMLKeptOnce`, `operation.TestParameter_BranchKeptWholeOnce`,
  `schema.TestDiscriminator_KeptWholeOnce`

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
`text/*` and `*/*` are Contents like any other. A key of the `content` map of a Request Body,
Response, Parameter or Header Object is a media type or a media type range (RFC 9110 §8.3.1,
§12.5.1): a type and a subtype, each a token, a wildcard either side and parameters after a `;`
admitted. Load's rule table reports a key that is neither as an `openapi/validation` warning of the
class `validation-invalid-syntax` at the key ([`validation`](#validation)), and the entry lowers as
written.

- Cases: `media-type-spelling`, `multi-content`, `component-media-types-32`
- Codes: `openapi/validation`
- Tests: `oas.TestCheck_ContentKeyIsAMediaType`, `operation.TestContent_AllMediaTypesKeptInOrder`,
  `operation.TestContent_MixedCaseFormContentKeepsEncoding`,
  `service.TestNormalizeMediaType_CollapsesTheSpellingsOneMediaTypeHas`,
  `operation.TestContent_AMediaTypeRefHoldingNoStringLowersNothing`,
  `naming.TestAt_NamesASchemaAfterItsOwner`

### `request-body`

A request body lowers to the operation's request `Payload`: each media type of its `content` to a
`Content` ([`media-types`](#media-types)), its `required` to `Payload.Required`, always set, since
OpenAPI's own default makes an undeclared `required` mean false rather than unstated, and its
`description` to `Payload.Docs`. A request body whose `content` map is empty, which every minor's
schema admits and 3.2 says SHOULD hold an entry, lowers to a `Payload` with no Contents that keeps
all three and its `x-*`, reported as an `openapi/empty-request-content` warning at the map; a
`content` left out is [`minor-required-fields`](#minor-required-fields)'.

A request body lowers on every method, the method compared as written, as HTTP compares one (RFC
9110 §9.1). On `GET`, `HEAD` or `DELETE`, for which HTTP defines a body no semantics (RFC 9110
§9.3.1, §9.3.2, §9.3.5), it is reported as an info `openapi/body-without-semantics` at the
operation's `requestBody`: OpenAPI 3.0 has a consumer ignore it and later versions advise against
it, but APIs do take one, the golden spec declaring `DELETE` bodies its API reads, so a warning
would rate a working operation a likely mistake. On `TRACE` or `CONNECT`, the latter reachable only
as a 3.2 `additionalOperations` key, HTTP has a client send no body (§9.3.8: "A client MUST NOT
send content in a TRACE request"; §9.3.6: a CONNECT request "does not have content"), so the
document likely says other than its author means: a warning `openapi/body-forbidden-by-method` at
the `requestBody`. `OPTIONS` draws neither, as RFC 9110 leaves its content to the resource.

- Cases: `empty-request-content`, `request-body-docs`
- Codes: `openapi/empty-request-content`, `openapi/body-without-semantics`,
  `openapi/body-forbidden-by-method`
- Tests: `operation.TestContent_NonRequiredRequestBody`,
  `operation.TestLowerRequestBody_AnEmptyContentKeepsTheBody`,
  `operation.TestRequestBody_OnAMethodWithoutSemantics`,
  `operation.TestRequestBody_OnAMethodThatForbidsOne`, `e2e.TestBodies_ThreeFindingsOfOneDocument`,
  `e2e.TestBodies_AMethodThatForbidsOne`

### `file-bodies`

A content entry is a file body, `Content.File` with `IsText` false and `ContentTypes` the key as
written, where its schema declares raw bytes or leaves its media type to say, so that a body to
stream is not read as a model. The schema is read through a `$ref`, and through the branches of an
`anyOf` or `oneOf` it writes, as the least any of them admits: a branch admitting no value but
`null`, in any spelling ([`null-alone`](#null-alone)) and writing no other keyword but one a file's
schema may (below), or none as `false` does, is passed over, and one that is itself such a union
says the body is something else. A schema declares
raw bytes with `format: binary` on a `type` that includes `string` or on none, and no
`contentEncoding`, which makes them encoded text as the type lowering reads it, read through a
refinement ([`allof-refinement`](#allof-refinement)) to the schema deciding it, whatever the media
type. It leaves the media type to say where it is absent, `true`, or writes no keyword but `type` as
`string`, alone or with `null`, the annotations `title`, `description`, `$comment`, `example`,
`examples`, `default`, `deprecated`, `readOnly`, `writeOnly`, `externalDocs`, `xml` and `nullable`,
the identifiers `$id`, `$anchor` and `$dynamicAnchor`, the dialect keywords `$schema` and
`$vocabulary` and the store `$defs`, which say nothing of the value, since naming the dialect a
schema is read in changes how its other keywords read and not what they admit, as a JSON Schema
draft's spellings of them do where the schema's dialect defines one,
draft 4's `id`, the drafts' `definitions` and 2019-09's `$recursiveAnchor`, the length bounds
`maxLength` and `minLength`, which OpenAPI applies to a binary payload's octets,
`contentMediaType`, which a media type set by the content's key overrides, and its own `x-*`, and
where each `$ref` on its chain writes beside it no keyword but those, `type` aside, since such a
keyword holds with what the `$ref` names, beside a 3.0 `$ref` too as the schema lowering applies one
([`later-version-construct`](#later-version-construct)); a chain is followed up to a bound of its
own, and one longer says the body is something else. Such a body is a declared file on
`application/octet-stream`, matched on the normalized key ([`media-types`](#media-types)), since RFC
2046 defines that type as arbitrary octets, and a file the binary policy reads on a media type it
names ([`binary-media`](#binary-media)), where `FileInfo.Inferred` is `binary-inference`. Any other
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
types are binary, so the reading is a guess and each file it makes carries `binary-inference` in
its `Inferred`. An entry `type/*` matches each media type of that type whose subtype has no
structured suffix, since a suffix names the syntax a body is written in: `image/*` matches
`image/png`, not `image/svg+xml`. An entry starting with `+` names a structured suffix.
`application/octet-stream` is no entry: a file it carries is declared, whatever the policy says. The
policy is the Go option `Options.BinaryInference`: `Disabled` turns the reading off, and `MediaTypes`
replaces the default list instead of extending it. The `binary-media` setting sets `MediaTypes`, its
entries separated by commas, and refuses one that is neither a media type nor a suffix, and
`binary-inference=false` sets `Disabled`.

- Cases: `file-media`
- Tests: `service.TestMediaTypeBinary_AnswersFromThePolicy`,
  `openapi.TestDecodeOptions_TextReachesEveryField`

### `example-values`

A media type's, parameter's or header's `example` lowers to an `Example` holding its value, ahead of
each entry of its `examples` map in order, each an `Example` named by its key: `summary` and
`description` as written, `value`, or 3.2's `dataValue`, as `Value` ([`values`](#values)), and
`externalValue` as `ExternalURL`. 3.2's `serializedValue`, which `ir.Example` has no field for, is
kept verbatim in the example's `Unmodeled` under `openapi:serializedValue` (`no_ir_home`), reported
as an info `openapi/degraded-construct` at it. An entry keyed by the empty string lowers unnamed,
as `Example.Name` reads `""` as no name, so the key is kept on the example's own `Unmodeled` under
`openapi:examples/key`, the value `""` (`degraded_lowering`), located at the entry, and reported there
as an info `openapi/degraded-construct`. No key of the Example Object can spell that entry, a `/` in
one being escaped, so an undeclared `name` it writes keeps `openapi:name` beside it
([`unknown-keys`](#unknown-keys)). A schema's examples are [`schema-examples`](#schema-examples)'.

- Cases: `examples-32`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestExamples_EmptyKeyIsKept`,
  `operation.TestExamples_AnEmptyKeyAndAWrittenNameAreKeptApart`,
  `e2e.TestExamples_AnEmptyKeyChangesTheDocument`

### `item-schema`

3.2's `itemSchema` lowers to `Content.Item`, the type of each item of a sequential body, with
`Content.Stream` set and no `Inferred`, since it declares the stream, and `itemEncoding` to
`Content.ItemEncoding`, with `Multi` set. In a multipart body its `Filename`, and
the `Inferred` beside it, are read off `itemSchema` and the `itemEncoding`'s `contentType` as a
multipart part's declaration is read ([`multipart-encoding`](#multipart-encoding)), an item being
one value where an array part is many, so an array item is no file; an item no `itemSchema`
describes is none either. Each item of a multipart body an `itemSchema` describes is a part, so such
a body gets an `ItemEncoding` where it writes no `itemEncoding`, and one naming no content type
takes the default of its item, as a part's entry does. An `itemEncoding` under a media type that is
not multipart, where OpenAPI gives it no effect and load warns of it ([`validation`](#validation)),
is kept whole under the content's `Unmodeled` at `openapi:itemEncoding` (`no_ir_home`), lowering
nothing, and reported there as an info `openapi/degraded-construct`. Beside a positional
`prefixEncoding`, a single every-item encoding cannot state ordinals, so `itemEncoding` and
`prefixEncoding` are both kept verbatim in the content's `Unmodeled`, under `openapi:itemEncoding`
and `openapi:prefixEncoding` (`no_ir_home`), reported as one info `openapi/degraded-construct` at
the content, while `itemSchema` still lowers to `Content.Item`. On a media type whose body is one
document, JSON's or XML's by name or `+json` or `+xml` suffix, and which the streaming policy
([`streaming-media`](#streaming-media)) does not name, `itemSchema` has no sequence to describe: it
lowers all the same, with a warning `openapi/item-schema-on-document` at it. Which other media types
repeat a structure is no closed set, so nothing else draws one.

- Cases: `sequential-media`, `file-items-32`, `multipart-defaults-32`
- Codes: `openapi/degraded-construct`, `openapi/item-schema-on-document`
- Tests: `operation.TestStreaming_AnItemSchemaOnOneDocumentIsReported`,
  `operation.TestStreaming_AnItemSchemaIsItsFrameWhateverItHolds`,
  `operation.TestStreaming_AnItemSchemaDeclaresItsContentAStream`,
  `service.TestHoldsOneDocument_AnswersOnlyForJSONAndXML`,
  `operation.TestItemFile_ReadsTheItemAsAPartDeclarationIs`,
  `operation.TestContent_AnItemEncodingOutsideMultipartIsKeptWhole`,
  `operation.TestFillSequential_AnItemEncodingWithNoNodeOutsideMultipartKeepsNothing`

### `streaming-media`

A Content whose media type the streaming policy names — `application/json-seq`,
`application/jsonl`, `application/x-ndjson`, `text/event-stream` and the `+json-seq` structured
suffix by default, matched on the normalized key ([`media-types`](#media-types)) at every minor —
sets `Content.Stream`, its `Inferred` the marker `streaming-inference`, wherever it is written: a
request body, a response, an error response, the default, a callback's or a webhook's. A Content
declaring an `itemSchema` sets it with no marker ([`item-schema`](#item-schema)), and any other
Content leaves it unset, so a response offering `application/json` beside `text/event-stream` marks
the event stream alone. A request body or success response with a Content that streams lowers to
`Operation.RequestStream` and `ResponseStream` for the directions that have one, each with `Events`
what one frame holds: the `Elem` of a `Type` whose root, read through aliases, is a list, since a
schema applies to the whole body and OpenAPI 3.2 §4.14.3.1 maps a sequential body to an array of its
values, else the `Type` itself, a schema with no array to read being the frame, or `bytes` on a file
body ([`file-bodies`](#file-bodies)), whose schema describes the file. `Content.Type` keeps the
list. `Streaming` is the `client`, `server` or `bidi` summary of them, and the operation's
`Provenance.Inferred` lists the markers of the Contents it reads, joined by `,` to any other marker,
since below 3.2 a media type is a guess about framing and not a declaration. A 3.2 `itemSchema` is a
declaration: it streams on any media type with `Events` the `Item` type and no marker, and wins over
the media-type reading at its own Content. An element settles once the registry is whole, since a
`$ref` can name a list nothing has built yet. Streaming Contents whose frames differ leave `Events`
unset on a direction that still streams, every Content kept, reported as an info
`openapi/degraded-construct` at the `requestBody` or `responses` pointer; Contents whose frames are
one element elect it, a list of a type agreeing with that type. Error responses and the default set
no `StreamDetail`, their Contents marked all the same: whether the default answers a success is the
plan's to decide ([`response-conditions`](#response-conditions)). The policy is the Go option
`Options.StreamingInference`: `Disabled` turns the media-type reading off, on the Contents and the
operation alike, leaving only what an `itemSchema` declares, and `MediaTypes` replaces the default
list instead of extending it, an entry starting with `+` naming a structured suffix. The
`streaming-media` setting sets `MediaTypes`, its entries separated by commas, and refuses one that
is neither a media type nor a suffix, and `streaming-inference=false` sets `Disabled`.

Two promotion targets ([`extension-promotion`](#extension-promotion)) fill the rest of a
`ResponseStream` once the document is whole, each key kept as written. `stream.terminator`, read by
default from `x-speakeasy-sse-sentinel`, fills `Terminator` with the text a key states: on the
operation, recorded in its `Provenance.Inferred`, or on a streaming Content of a success response,
recorded in the Content's `Inferred`, where real documents write it. It is filled all or none: the
operation's text, where no streaming Content states another, else the one text every streaming
Content of the success responses states. A Content stating another, one stating none beside one
that states it, or a key on an operation streaming no response fills nothing, and one info
`openapi/unpromoted-extension` names the key, the later by pointer where Contents disagree. An empty
text, or a value that is no text, states nothing and is reported at its key. `stream.condition`,
with no default source, reads an expression of the request (`$request.body#/stream`, or the
shorthand `$request.stream`, read as a pagination input's is) into `Condition`, the input asking for
the stream when true, recorded in the operation's `Provenance.Inferred`: only where the input's
type stands for the boolean primitive, the operation streams a response, and a success response
offers a Content that does not stream beside one that does. Any other key is reported at itself.

- Cases: `streaming-media-30`, `streaming-media-31`, `stream-terminator`
- Codes: `openapi/degraded-construct`, `openapi/unpromoted-extension`
- Tests: `operation.TestStreaming_ResponseMediaTypeImpliesServerStream`,
  `operation.TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed`,
  `operation.TestStreaming_ItemSchemaDeclaresTheStream`,
  `operation.TestStreaming_AFileBodyStreamsBytes`,
  `operation.TestStreaming_AListBodyStreamsItsElement`,
  `operation.TestStreaming_ContentsCompareByTheirFrames`,
  `operation.TestStreaming_EachContentSaysWhetherItStreams`,
  `operation.TestStreaming_EachContentStreamsInEitherOrder`,
  `operation.TestStreaming_ACallbackAndAWebhookMarkTheirContents`,
  `operation.TestStreaming_AMediaTypeRefStreamsByTheKeyItIsWrittenUnder`,
  `e2e.TestExternalContent_AStreamReadsAListAnotherDocumentHolds`,
  `service.TestMediaTypeStreams_AnswersFromThePolicy`, `promotion.TestTextOf_ReadsTheFirstSourceStatingAText`,
  `stream.TestPromote_FillsTheTerminator`, `stream.TestPromote_FillsTheTerminatorOfEveryStreamingContent`,
  `stream.TestPromote_TerminatorIsAllOrNone`, `stream.TestPromote_OperationAndContentsMustAgree`,
  `stream.TestPromote_FillsTheCondition`, `stream.TestPromote_AConditionNeedsAStream`,
  `stream.TestPromote_ReadsPastAResponseWithNoBody`, `runtimeexpr.TestRequestValue_ResolvesEachSpelling`

### `multipart-encoding`

An Encoding Object of a `multipart/*` or `application/x-www-form-urlencoded` Content lowers to a
`PartEncoding` in `Content.Encoding`, keyed by the `PropID` of the body property its key names,
wherever the body declares that property: on the schema, in an `allOf` branch with the first
declaration of a name winning, on a model it composes, or on a `oneOf` or `anyOf` branch, written on
the schema or on an `allOf` branch, which the schema lowering distributes
([`union-beside-structure`](#union-beside-structure)). Where several models declare the part, the
key is the declaration the body's flattening takes ([models](../ir/models.md#the-flattening)): a
model's over one it composes, else the later conjunct's, so `allOf: [{$ref: A}, {$ref: B}]` keys
`B`'s. A body that is a union keys a part under each variant declaring it, through the unions nested
in its variants, the entry written applied to each and the structural flags and default content type
read off that variant's declaration alone, so a part a file in one variant and text in another is
both, each where it is declared. A schema declaring the part is read for one variant's declaration
but where it declares another variant's, matched by the `PropID` its place gives it, so a
declaration the `allOf` merge folds into a variant's is read with it. A body, or a variant, that is
a model narrowing a union, as one composing a union beside a member of its own is, keys a part so
under each shape it takes ([models](../ir/models.md#the-shapes-of-a-model-narrowing-a-union)), the
declaration that shape's flattening takes, and one whose shapes `ir.NarrowedShapes` does not list,
or not within a bound of its own, is reported as a warning `openapi/degraded-construct` at the body
schema, since a part only its union's variants declare then has no key. The parts are read by a walk
of the body's schema: the properties the schema its `$ref` chain ends on declares, then each `allOf`
branch's, as [`allof-classification`](#allof-classification) flattens them, with the same cap, then
each `oneOf` and `anyOf` branch's, of the schema and of each `allOf` branch. A `$ref` entry and a
union branch are followed up to a bound of their own, and entered again only by a shorter path. A
bound that cuts a target no path enters is reported as a warning `openapi/degraded-construct` at the
body schema, since a part reached only past it has no `Content.Encoding` key. A union nested past a
bound of its own in the body's type is reported so too.

Each entry that keys nothing is kept whole, as written, under the content's `Unmodeled` at
`openapi:encoding/<key>` (the key escaped as a JSON Pointer token), located at the entry, and
reported there as an info `openapi/degraded-construct` saying why. Its reason follows its effect: an
entry the walk decides names no property, every entry of a file body, which has no parts, and every
entry under a media type that is neither multipart nor form has none where it is written and no node
is coming for it, so it is kept under `out_of_scope`
([boundaries](../ir/boundaries.md#deliberate-exclusions)); an entry the walk cannot decide, as a
catch-all may take its key, and one of the second kind below, under `no_ir_home`:

1. A key naming no part the walk reads: no property, a map's or a catch-all's entry, a part past a
   bound, or any entry of a body declaring no property, such as one with no schema, one whose schema
   `$ref` does not resolve, or a file body ([`file-bodies`](#file-bodies)). Nothing such an entry
   declares is lowered: its header schemas register no type, and its `x-*`, unknown keys and nested
   encodings have no record of their own, though load's rule table judges its fields, an unknown key
   or a reserved header name in it among them ([`unknown-keys`](#unknown-keys),
   [`reserved-header-names`](#reserved-header-names)). Where the walk reads every
   position of the body's schema that can declare a property, and no catch-all there admits a member
   the schema names no property for, the key names no property, which OpenAPI requires of every key
   (3.0.4 and 3.1.1: "The key, being the property name, MUST exist in the schema as a property"),
   giving such an entry no effect (3.2.0: "`encoding` entries with no corresponding property SHALL
   be ignored"); it is reported besides as a warning `openapi/unknown-encoding-key` at the entry. A
   catch-all is an `additionalProperties` or `unevaluatedProperties` other than `false`, or a
   `patternProperties`; the walk reads nothing written beside a `$ref`, past one resolving to
   nothing, through a `$dynamicRef`, or under `if`, `then`, `else` or `dependentSchemas`. A key of
   the nested `encoding` a 3.2 Encoding Object writes, which a part's record keeps verbatim
   ([`unmodeled-dialect-fields`](#unmodeled-dialect-fields)), is held to the properties of the value
   it applies to, an array part's items, and draws the same warning at its own entry.
2. A key naming a part the walk reads that the type the body's schema lowered to holds no property
   for: one declared beside a keyword lowering the body, or a union branch, to no model, as a
   `const`, an `enum` or a scalar `type` does, where the schema lowering keeps the `properties`
   verbatim ([`shape-applicator`](#shape-applicator)). Its fields were lowered before the type was
   known, so its header schemas stay registered with nothing referencing them; its other records are
   the entry kept whole, or, where the entry cannot be kept, the records its lowering made.
3. Any entry of an `encoding` written under a media type that is neither multipart nor form, where
   OpenAPI gives the field no effect, a file body's among them.

A key naming a catch-all's member has no typed home, `*` included, by decision. OpenAPI makes an
encoding key a property name, so `*` names the one member called `*`, and gives an entry naming no
declared property no effect; reading `*` as every member a catch-all admits is a convention no
version states, which a typed field would take as a guess, and a field keyed by the key as written,
beside the `PropID`s, would type a misspelt key alike.

What OpenAPI does say of the parts a catch-all admits, their structural flags and default content
type, lowers to `Content.CatchAllEncoding` ([payloads](../ir/payloads.md#a-catch-alls-parts)), keyed
by the type holding the catch-all: the body's type where it is a map, and otherwise each model the
flattening of the body's type reads that holds a catch-all, a union body's variants each read so, as
`ir.CatchAllHolders` lists them and `pass.Validate` holds them. `Value` is the encoding of a part no
pattern matches, `additionalProperties`' or `unevaluatedProperties`', and `Patterns` one per
`patternProperties` entry, each read as a declared part with no encoding entry is: `Multi` and
`Filename` off the value's type, and its default content type below, `contentEncoding` read off
every catch-all value schema the part walk meets. An encoding that says nothing is not written, and
a holder whose parts say nothing gets no key. A body that is a map declares no part, so every form
body stating a schema is settled, though the walk finds no part.

A `multipart/form-data` or `application/x-www-form-urlencoded` body stating a schema that is no
file body, and whose type, read through its unions, stands for no model, no map, no object value and
not the top type, names no part to send, as an array of strings does, where an object `const` names
its parts by its members: it is reported as a warning
`openapi/partless-form-body` at the schema and lowers as declared. A union read past its bound is
not judged, and nor is a type reaching no node, which load reports.

`contentType` lowers to `ContentTypes`, split on `,` and trimmed with empty items dropped, and one
naming no media type is an `openapi/validation` error ([`validation`](#validation)); and `headers`
to `Headers`, lowered as a response's are, under a multipart media type, the only kind OpenAPI
applies them to (3.0.4: "This field SHALL be ignored if the request body media type is not a
`multipart`"). A `style`, an `explode` or an `allowReserved` takes effect in a form body at every
minor, and in a `multipart/form-data` body from 3.1 (3.1.1: "This field SHALL be ignored if the
request body media type is not `application/x-www-form-urlencoded` or `multipart/form-data`"). Where
one is written and takes effect, it arranges the value as a query parameter's is, which replaces the
content type, implicit or explicit: `Style` lowers to the style written or else to `form`, a query
parameter's default (3.0.4: "If any of `style`, `explode`, or `allowReserved` are _not_ present with
explicit values, then they are to be treated as if they were present with their default values"),
`Explode` as written, nil reading as the style's default, and `AllowReserved` as written. A field
written where it takes no effect is typed nowhere: it is kept verbatim under the content's
`Unmodeled` at `openapi:encoding/<key>/<field>` (`no_ir_home`) and reported there as an
`openapi/validation` warning of the class `validation-no-effect`, and its part reads as one whose
entry does not write it. A part whose entry writes no `contentType`, or that has no entry, takes the
default OpenAPI gives it in `ContentTypes`, unless its entry arranges its value. An entry writing an
empty `contentType`, which load reports, takes none. A response's part takes its default too, at
every minor, though before 3.2 OpenAPI applies `encoding` to request bodies alone: the default is
what OpenAPI gives a part by its schema, which needs no Encoding Object to apply. The default is
read at the document's minor off the types the part's declarations settled on, the ones `Multi` and
`Filename` are read off: each value an Encoding Object applies to, which is each item of an array
part, the element of a list or tuple and a union's members in its place, takes the row its kind
names, and the part takes the one every value takes, a null value passing over:

1. an object, a model or a map, takes `application/json`;
2. a number, an integer, a boolean, and a string, a format's primitive among them, take
   `text/plain`, as the bytes `format: base64url` sends as text do, which 3.0's table keys as a
   string of a format other than `binary` or `byte`, and as an enum or a `const` of strings does
   where it writes `format: binary` or `byte`, which its type has no home for and keeps under
   `Unmodeled`;
3. raw bytes, which every minor's lowering reads `format: binary` as, take
   `application/octet-stream`, as 3.0's row for `binary` and the later minors' row for raw binary,
   which their own multipart example gives a string of format `binary`, say;
4. a string of `format: byte` takes `application/octet-stream` in 3.0, and `text/plain` from 3.1,
   where the format has no effect on the content encoding; one whose declarations write
   `contentEncoding` takes `application/octet-stream`, and any other string writing it does from
   3.1, and `text/plain` in 3.0, whose table keys a string on its format;
5. a value whose schema writes no type, lowered to `any`, takes `application/octet-stream` from 3.1,
   and none in 3.0, whose table has no such row;
6. an array inside an array part takes `application/json` in 3.2, whose table gives it that row, and
   before 3.2 the row of its items, as the table defers an array's to them.

A part whose values take different defaults, or one a row gives none, such as a value of a kind no
row names or a string whose declarations write `contentEncoding` and do not alike, takes none. Each
item of a multipart body's `itemSchema` is a part too, and an `itemEncoding` naming no content type,
or none written, takes its item's default, a list item being an array inside the array; such a body
gets an `ItemEncoding` where it writes none ([`item-schema`](#item-schema)).

`Multi` and `Filename` come from the part, read once every type has lowered, and `Filename` also
from the media types its encoding entry names; no part of a url-encoded body is a file, since the
body has no parts to carry a file name. Where the body's type has the property and its type is a
list, a tuple, a primitive, or a scalar whose bases end in one of those or in no base, they are read
off that type, the one the `allOf` merge settled on: `Multi` where it ends in a list or tuple,
`Filename` where it ends, in a multipart body, in `bytes` no encoding such as `format: byte`
re-encodes, or in a list or tuple every element of which does, a tuple's rest included where
elements may follow its prefix. A union is read off its variants, through the unions nested in them,
each as such a type is and one of another kind as neither: `Multi` where any is, since a part that
may repeat travels as repeated parts, and, in a multipart body, `Filename` where every one is
`bytes` no encoding re-encodes or a list or tuple of them. A property of any other kind, such as an
enum, literal, model or map, or a union reaching more nested unions than a bound of its own, reads
off its type as no file, as such a member of a union does, and its `Multi` is read from every schema
declaring it: where any is an array, or writes no `type` and a union with an array among its
branches. In a multipart body a part is a file too where every schema declaring it reads so as a
body's schema does ([`file-bodies`](#file-bodies)), an array through each of its elements, its
`prefixItems` and the `items` after them, or a draft's list of `items` and the `additionalItems`
after them ([`tuples`](#tuples)), a rest of `false` admitting none, as is an array branch of a union
it writes, whose other branches are read as a body's union branches are: one declaring raw bytes;
one leaving its media type to say, where its encoding entry's `contentType` names only
`application/octet-stream` or media types the binary policy names ([`binary-media`](#binary-media)),
`PartEncoding.Inferred` being `binary-inference` where it names one of the latter; and, from
OpenAPI 3.1 and with no `contentType`, one writing no `type`, whose default content type OpenAPI
makes `application/octet-stream`, a union being read so only where each of its branches, an array's
through its elements, writes no `type` or declares raw bytes, so that
`{anyOf: [{}, {type: string}]}` reads as a string. A `string` part with neither is `text/plain`,
whatever its `contentMediaType`, which OpenAPI ignores where an encoding's default disagrees, and an
array writing no `items`, or a draft's tuple no `additionalItems`, says nothing of its elements past
its prefix, so neither is a file. A part is entered only when its `PartEncoding` holds something, so
a body that declares no encoding still gets entries for its parts, and an entry declaring nothing
for a part whose type takes no default leaves none. What an Encoding Object writes beyond those
fields is [`encoding-allow-reserved`](#encoding-allow-reserved). An encoding written where OpenAPI
gives it no effect is reported as an `openapi/validation` warning, of the class
`validation-no-effect`, at the field ([`validation`](#validation)): an `encoding` under a media type
that is neither multipart nor form, or before 3.2 outside a request body, and a 3.2 `prefixEncoding`
or `itemEncoding` under one that is not multipart. A media type is read without its parameters and
in any case.

- Cases: `multipart-encoding`, `file-media`, `encoding-unkeyed`, `multipart-defaults-30`,
  `multipart-defaults-31`, `multipart-defaults-32`, `encoding-union-branches`, `encoding-outside-form`,
  `catch-all-encoding`
- Codes: `openapi/degraded-construct`, `openapi/unknown-encoding-key`, `openapi/validation`,
  `openapi/partless-form-body`
- Tests: `operation.TestContent_MultipartPartEncoding`,
  `oas.TestCheck_EncodingsApplyWhereTheSpecificationSays`, `oas.TestEncodingRules_AnswerAsTheTableDoes`,
  `operation.TestContent_ArrayMultipartPartMulti`,
  `operation.TestPartEncodings_MixinPartIsKeyedOnTheMixin`,
  `operation.TestPartEncodings_KeyedOnTheMemberTheBodyDeclares`,
  `operation.TestPartEncoding_FlagsComeFromTheMergedType`,
  `operation.TestPartEncoding_FlagsOfAnUndecidedTypeComeFromTheSchema`,
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
  `schema.TestListedTuple_ReadsADraftsEntriesAndRest`,
  `operation.TestPartEncodings_AnEntryNamingNoPartIsKeptWhole`, `operation.TestKeepEntry_ReasonFollowsEffect`,
  `operation.TestPartEncodings_AnUnkeyedEntryLowersNothingItDeclares`,
  `operation.TestPartEncodings_AnUnkeyedEntryThatIsNoObjectAddsNothingToLoadsError`,
  `operation.TestPartEncodings_AnEntryThatCannotBeKeptIsReportedNotAnnounced`,
  `operation.TestPartEncodings_KeptEntriesDoNotDependOnDeclarationOrder`,
  `operation.TestPartEncodings_AKeptEntryNeverTakesAPartsRecord`,
  `operation.TestPartEncodings_AnEntryNamingNoPartIsKeptWholeInCallbacksAndWebhooks`,
  `operation.TestPartEncodings_AnEncodingOutsideAFormIsKeptWhole`,
  `operation.TestPartEncodings_APartTheBodysTypeHoldsNoPropertyForKeysNothing`,
  `operation.TestPartEncodings_AUnionBranchsPartIsKeyed`,
  `operation.TestPartMembers_AUnionBodyHasItsVariantsMembers`,
  `operation.TestMemberHolders_StopsPastItsBound`,
  `oas.TestEncodingRules_TakesEffectWhereTheTextSays`,
  `operation.TestPartEncodings_ANestedKeyNamingNoPropertyIsWarnedOf`,
  `operation.TestPartEncodings_AnEntryKeptAtSettlementThatCannotBeKeptKeepsItsRecords`,
  `operation.TestPartEncodings_AUnionPastItsBoundIsReported`,
  `operation.TestPartWalk_DecidesKeysOnlyWhereItReadsEveryDeclaration`,
  `operation.TestPartWalk_ReadsARefChainToItsBound`,
  `operation.TestDeclaredMulti_ReadsEveryDeclaration`,
  `operation.TestDefaultContentType_ReadsEachValueByItsRow`,
  `operation.TestDefaultContentType_ReadsUpToItsBound`,
  `operation.TestPartValues_ReadsEachItemOfEachDeclaration`,
  `operation.TestReadContentEncoding_ReadsEveryDeclaration`,
  `operation.TestPartEncodings_AContentTypeWrittenTakesNoDefault`,
  `operation.TestEncoding_ArrangingFieldsLowerWhereTheyTakeEffect`,
  `operation.TestEncoding_MultipartHeadersLower`,
  `operation.TestPartEncodings_AUnionBodyKeysEachVariantInEitherOrder`,
  `operation.TestPartMembers_AModelNarrowingAUnionHasItsShapesMembers`,
  `operation.TestShapeFlattenings_ReadsShapesWithinItsLimit`,
  `operation.TestPartEncodings_AKeptEntryIsPlacedInTheDocumentDeclaringIt`,
  `operation.TestPartKeys_KeysACatchAll`, `e2e.TestCatchAllEncoding_IsKeptAndVerifies`,
  `operation.TestPartless_FormBodyWithNoObject`, `operation.TestPartless_JudgesWhatTheBodyNames`

### `encoding-allow-reserved`

An Encoding Object's `allowReserved` lowers to its `PartEncoding.AllowReserved`, on the part's entry
or on the content's `ItemEncoding`, `false` where it declares none, as a parameter's lowers onto its
binding, where it takes effect ([`multipart-encoding`](#multipart-encoding)). A part declaring it
and nothing else keeps its `PartEncoding`, its `Style` the default `form`, which a part declaring no
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
`validation-allowed-values` ([`validation`](#validation)). A `style` or `explode` beside the
header's `content`, where neither has an effect, lowers as declared and is an
`openapi/inapplicable-location-keyword` warning at the key
([`content-parameters`](#content-parameters)). A `Content-Type` entry in either headers
map, which OpenAPI says SHALL be ignored, lowers as declared and is reported as an
`openapi/reserved-header-name` warning at the entry, as a parameter is
([`reserved-header-names`](#reserved-header-names)).

A key of either headers map is the header's name, so an HTTP field name: one that is no token, the
empty key and one holding a space among them, lowers as declared and is an `openapi/validation`
error of the class `validation-invalid-format` at the entry, as a header parameter's `name` is
([`validation`](#validation)). Two keys of one map naming one header in different case, which HTTP
compares in any case, both lower, and each but the one lowest in byte order is an
`openapi/validation` warning of the class `validation-duplicate-key` at its entry, so which is
reported follows the spellings and not the order the map writes them in.

- Cases: `header-content-schema`, `header-name-case`, `header-name-syntax`
- Codes: `openapi/reserved-header-name`, `openapi/validation`,
  `openapi/inapplicable-location-keyword`
- Tests: `operation.TestHeaders_SerializationKeywordsLower`,
  `operation.TestHeaders_SerializationKeywordsAbsentRecordNothing`,
  `operation.TestHeaders_SerializationKeywordsFollowTheDeclaration`,
  `operation.TestHeaders_ReservedContentTypeEntryIsReported`,
  `operation.TestHeaders_ReservedContentTypeInEncodingIsReported`,
  `oas.TestCheck_HeaderNamesAreFieldNames`

### `http-date-inference`

The HTTP-date policy, `Options.HTTPDateInference` and the text option `http-date-inference`
([`policies`](#policies)), gives a response header the text its field's definition gives a date. A
header of a response, an error case or the default, naming one field, whose name, compared without
case, is a field an RFC defines as an HTTP-date, or as one in its date form, `Date`,
`Last-Modified`, `If-Modified-Since`, `If-Unmodified-Since`, `Retry-After` and `If-Range` (RFC 9110
§6.6.1, §8.8.2, §13.1.3, §13.1.4, §10.2.3, §13.1.5), `Expires` (RFC 9111 §5.3) and `Sunset` (RFC
8594 §3), and whose type reads, through its scalars, as the `datetime_offset` `format: date-time`
lowers to ([`string-formats`](#string-formats)), with no `Encoding` on the header, as the media type
of one declared by `content` is, and no encoding named along that chain, takes an `Encoding` named
`http-date` ([encodings](../ir/constraints.md#encodings)) on the header itself. Such a name on such
a type is the policy's trigger, an encoding the document writes being where it has spoken, and it
declines nothing that meets it. A header declared `format: http-date` or `unixtime` carries the
encoding its format names on its type ([`string-formats`](#string-formats),
[`numeric-formats`](#numeric-formats)), so the document has spoken for it. It is an override of that
one use, so a component a `$ref` names keeps its own text wherever else it is used. An info
`openapi/inferred-http-date` at the header reports it, since the reading replaces the RFC 3339 text
`date-time` names, which the field's definition rules out. A header an encoding gives a part is a
MIME header no such definition covers, and is not read. On the golden spec one header is read, the
`Last-Modified` of
`GET /accounts/{account_id}/dex/commands/{command_id}/downloads/{filename}`, whose example is an
HTTP-date (spec @86556f6f).

A header parameter is read the same way, under the name its binding places it at, the wire name or
else its own, for the same fields, since `Expires` and `Last-Modified` are sent as well as returned:
where the parameter writes no `Encoding` and its type reads as such an instant, it takes the
`http-date` encoding on `Parameter.Encoding`, its own, so a `$ref`'d component it names, and every
other use of that component, keep their text ([operations](../ir/operations.md)). Its
`Provenance.Inferred` names `http-date-inference`, and an info `openapi/inferred-http-date` at the
parameter reports it once, however many operations a path item's parameter is copied into. A query,
path or cookie parameter is never read, whatever its name.

- Cases: `http-date-inference`
- Codes: `openapi/inferred-http-date`
- Tests: `convention.TestHTTPDate_AFieldItsRFCDefinesAsADate`,
  `convention.TestHTTPDate_ReadsEveryResponseAndNoPart`,
  `e2e.TestConventions_DeclarationOrderChangesNothing`, `e2e.TestConventions_DisabledFillsNothing`,
  `convention.TestHTTPDate_ReachesAHeaderParameter`, `convention.TestHTTPDate_ParameterRows`

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
   content must walk alike. A root or a member typed as an alias of a model, as a schema that is a
   `$ref` alone lowers, is read as that model, through a chain of at most 64 aliases, as every step
   reading the finished document reads one.
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
   the expression keeps the grammar or not and whether the string is the value or sits within a
   list or an object the value holds, or a constant the IR has no value for.

- Cases: `response-links`, `body-alias`
- Codes: `openapi/invalid-link`, `openapi/untyped-link`
- Tests: `operation.TestLinks_AFindingKeepsTheLinkWhole`, `operation.TestLinks_ResolvedFormsLower`,
  `operation.TestLinks_EveryValueKindLowers`, `operation.TestLinks_ErrorsAndDefaultsCarryLinks`,
  `operation.TestLinks_ASharedResponseResolvesPerOperation`,
  `operation.TestLinks_MediaTypesDisagreeingOnAMember`,
  `operation.TestLinks_AHeaderTwoCasesDeclareIsNeither`,
  `operation.TestLinks_AHeaderIsReadOffItsOwnResponse`,
  `operation.TestLinks_LowerAlikeInEitherDeclarationOrder`,
  `operation.TestLinks_ReadAMemberThroughAnAlias`,
  `operation.TestLinksResolve_AllocatesLinearInWhatItReads`,
  `runtimeexpr.TestParse_ReadsTheGrammar`,
  `runtimeexpr.TestEmbeds_FindsAnExpressionInBraces`,
  `runtimeexpr.TestEmbeds_AllocatesNothing`,
  `runtimeexpr.TestBodyPath_WalksEveryContentToTheSameMembers`,
  `whole.TestDocument_MemberAgreesWithMembers`,
  `whole.TestDocument_WalkNamesEachMemberFromTheRoot`,
  `whole.TestDocument_HeaderMatchesTheNameWithoutCase`,
  `whole.TestDocument_ReadsOnceAndAllocatesNothingAfter`,
  `whole.TestDocument_ABodyReadFlattensNoModel`,
  `whole.TestDocument_MembersFlattensAModelOnceWhateverNamesIt`,
  `e2e.TestBodyAlias_ReadsTheSameInEitherOrder`,
  `e2e.TestBodyAlias_ReadsAnOperationInANestedGroup`,
  `openapi.TestRun_ReadsTheFinishedDocumentOnce`, `openapi.TestRun_AppendsNothingAfterAFirstAsk`,
  `e2e.TestUnpreservable_AnnouncementNeverOutrunsTheEntry`
- Status: partly built: a key beside a link's `$ref` other than `summary` and `description` is
  dropped with no record, as beside any other reference (#1321). A body whose model composes `m`
  models, read for `n` distinct members, costs `n` walks of the composition where a flattening
  would cost one (#1955).

## 12. Webhooks and callbacks

### `webhooks`

Each operation of a `webhooks` entry's path item lowers to an `Operation` mounted at its own
pointer, in the synthesized `webhooks` group, its HTTP binding's `Inbound` a webhook, with the
entry's key as `WebhookName` and no `URITemplate`, since the key names an event rather than a path.
Its `Auth` is what its own `security` states, and nothing when it writes none: the document's
`security` describes requests clients send, and a webhook is one the API sends
([auth](../ir/auth.md#inbound-operations)). Its `Servers` are likewise what it and its path item
state ([`path-item-servers`](#path-item-servers)), where the webhook is sent, and nothing of the
document's ([servers](../ir/servers.md#inbound-operations)).

- Cases: `webhooks`
- Tests: `operation.TestOperations_InboundOperationsNameOnlyTheServersTheyState`

### `callbacks`

An operation's `callbacks` lower to its HTTP binding's `Callbacks`: for each runtime expression of
each named callback, a `Callback` holding the callback's name and the expression as written and the
`OpID`s of the operations its path item declares. Each of those lowers to an `Operation` of its own,
mounted at its own pointer below the declaring operation's
(`…/callbacks/<name>/<expression>/<method>`, the name escaped as one token), filed in the declaring
operation's group whatever its own tags or grouping extension state ([`tag-groups`](#tag-groups)),
its binding's `Inbound` a callback, with the expression as its `URITemplate`
([http-binding](../ir/http-binding.md#inbound-operations)), and inheriting no `security` and no
`servers`, as a webhook does ([`webhooks`](#webhooks)). A callback's operation may declare
callbacks in turn, which OpenAPI also gives as requests the API sends, and they lower the same way,
listed on its binding and filed in the same group, the deeper operations after the one declaring
them, down to an operation nested in four callbacks. Past that bound, which stops a callback naming
itself through a `$ref` from lowering without end, the deepest operation's `callbacks` are kept
verbatim on its HTTP binding as `openapi:callbacks`, of reason `degraded_lowering`, and reported as
an `openapi/degraded-construct` warning at that `callbacks` map. Each level's `$ref`s are charged as
any are ([`source-budgets`](#source-budgets)), so one naming its callback several times over, whose
copies multiply at each level, is refused once they pass the budget. A key is a runtime expression, by
the grammar each minor gives, text embedding runtime expressions in braces, as OpenAPI's own example
does, or a constant URL, which embeds none; one that is none of them is reported as an
`openapi/validation` error, of the class `validation-invalid-format`, at the key
([`validation`](#validation)), and lowers as written.

- Cases: `callbacks`
- Codes: `openapi/validation`, `openapi/degraded-construct`
- Tests: `operation.TestCallbacks_NameWithSlashKeepsItsOwnKey`,
  `operation.TestCallbacks_DeclaredOnACallbackLower`,
  `operation.TestCallbacks_ACallbackNamingItselfStopsAtTheBound`,
  `openapi.TestCompile_ACallbackNamingItselfIsChargedAtEachLevel`,
  `operation.TestOperations_InboundOperationsNameOnlyTheServersTheyState`,
  `oas.TestCheck_CallbackKeysAreRuntimeExpressions`

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

A requirement's key names an entry of the source's `securitySchemes`, whichever document holds the
requirement, matched exactly, in the specification's order. A key that is the name of an entry is
that name, at every minor. From 3.2, which lets a key be the URI of a Security Scheme Object, any
other key is a reference, read against the document holding the requirement
([`references`](#references)), a bare word as a relative URI: one landing on an entry of the source's
`securitySchemes`, or naming one that resolves to nothing, names that entry, as its name would, and
one landing on a Security Scheme Object anywhere else, in the source or another document, names that
object, lowered once where it is declared, under the ID its position there derives
([identity](../ir/identity.md)), whichever requirement reaches it first. 3.0 and 3.1 define no URI
form, so there a key is a name alone. A name that reaches no mechanism lowers to a
scheme of kind `unresolved` ([auth](../ir/auth.md#unresolved-schemes)), one per name, and is
reported as an `openapi/unresolved-security-scheme` error at each requirement, quoting the key as
written. Where a declared scheme that reaches a mechanism is a near miss of the name, the error
names it, resolution staying exact: the first that holds of the same neutral name, the same letters
and digits in any case, and, for a name of 4 to 64 bytes where at most 64 declared schemes reach a
mechanism, a name one edit apart, an insertion, a deletion, a substitution or two adjacent runes
swapped, the least in byte order where several do. A key read as a reference that lands on nothing adds why,
in the words the reference's own report would use, and lowers to a scheme minted from the key and the
document writing it, so one key in two documents is two schemes. Nothing else
about the requirement changes: the option keeps its other schemes, its scopes and its place in the
list, so an operation stating its own requirement does not read as inheriting, and an option whose
name reaches no mechanism does not vanish from the options beside it. The error stands however the
rest lowers, since the specification requires the name to correspond to a scheme and what it means
is missing ([`validation`](#validation)). A name stands whatever is beside it, whether or not it
reaches a mechanism, since an option demands every scheme it names: a value that reads as no list of
strings lists no scopes, a warning from the rule table for a `null` and an error for any other
([`requirement-scopes`](../parser-library.md#requirement-scopes)). A scope is the text an item
writes, so a plain date is one ([`undecoded-scalar`](../parser-library.md#undecoded-scalar)). The
scopes an option lists for one scheme are a set: a scope written again is kept once, at its first
occurrence, and reported as an `openapi/repeated-entry` warning at the repeat, naming the first.
Scopes compare as the text written, and each scheme's list folds on its own.

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
A `$ref` that reached nothing is kept the same way, under `openapi:$ref`, with only its error, but a
`$ref` that names nothing, a null or a list among them ([`references`](#references)), leaves the
entry read as left out. An entry written as no object holds no field to keep. A `security` entry
written as no object names
nothing, and so does one with no member whose `<<`, or a `<<` of a mapping it merges at any depth,
names no mapping, which merges nothing ([`merge-keys`](../parser-library.md#merge-keys)): each
lowers to no option, never the empty one, which would grant access the document never wrote. A list
holding nothing else, though it writes entries, collapses to nil, beside the rule table's error,
since `[]` would read as public. A `security` that is no list lowers as one left out, beside the
rule table's finding, a warning for a `null` and otherwise an error.

3.2's device flow lowers to `Flows` beside the other flows, and `oauth2MetadataUrl` to
`OAuth2MetadataURL`. An `oauth2` scheme whose `flows` declares no flow — `flows: {}`, only `x-*` and
keys that name no flow, or only flows written null — and that has no `oauth2MetadataUrl` lowers as
declared, reported as an `openapi/oauth2-no-flow` warning at the declaration; a `flows` left out,
null or no mapping is the rule table's to report instead
([`minor-required-fields`](#minor-required-fields)). The table states the rules this one reports, a
requirement naming no scheme and a scheme naming no mechanism, and leaves them to it.

- Cases: `security-schemes`, `security-or-and`, `security-root-empty`, `security-unresolved`,
  `oauth2-no-flow`, `security-states`, `security-root-absent`, `security-root-optional`,
  `repeated-tags-and-scopes`
- Codes: `openapi/oauth2-no-flow`, `openapi/incomplete-security-scheme`,
  `openapi/unresolved-security-scheme`, `openapi/unresolved-ref`, `openapi/repeated-entry`
- Tests: `auth.TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismLowersAsUnresolved`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsWhatItWrote`,
  `auth.TestLowerSecuritySchemes_AKeptTypeIsTheEntrysOwn`, `auth.TestLowerSecuritySchemes_ABrokenRefIsKept`,
  `auth.TestSecurityRequirement_AnUnresolvedMemberKeepsItsOption`,
  `auth.TestSecurityRequirement_AMemberWhoseScopesDoNotReadStaysInItsOption`,
  `auth.TestSecurityRequirement_AScalarTheModelDoesNotDecodeIsAScope`,
  `auth.TestLowerSecuritySchemes_ARefTheModelDoesNotDecodeIsReported`,
  `e2e.TestSecurityUnresolved_ATaggedSchemeRefIsRefusedAndTheEntryLeftOut`,
  `auth.TestLowerSecuritySchemes_ABrokenRefKeepsTheDocsBesideIt`,
  `auth.TestSecurityRequirement_ASecurityThatIsNoListReadsAsLeftOut`,
  `oas.TestCheck_ANullBesideARequirementsNameListsNoScopes`,
  `e2e.TestSecurityUnresolved_ANameTwoDocumentsUseIsOneSchemeOfTheSource`,
  `auth.TestSecurityRequirement_AnOperationsOwnUnresolvedListDoesNotInherit`,
  `auth.TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil`,
  `auth.TestSecurityRequirement_AMergeNamingNoMappingIsNoOption`,
  `auth.TestSecurityRequirement_ASoleMergeNamingNoMappingCollapsesTheListToNil`,
  `auth.TestSecurityRequirement_AMergeChainIsReadToItsEnd`,
  `auth.TestSecurityRequirement_AJSONDocumentWritesNoMerge`,
  `auth.TestSecurityRequirement_ARepeatedScopeIsKeptOnce`,
  `auth.TestSecurityRequirement_A32KeyMayNameItsSchemeByURI`,
  `auth.TestSecurityRequirement_A31KeyIsAName`,
  `auth.TestSecurityRequirement_A32URIIsReadWhereverItLands`,
  `auth.TestSecurityRequirement_AURIIsReadInTheDocumentHoldingIt`,
  `auth.TestSecurityRequirement_ANameAndItsURIAreOneSchemeInEitherOrder`,
  `auth.TestSecurityRequirement_AKeyAnotherDocumentMintsStillNamesTheSourcesEntry`,
  `oas.TestSecurityRules_AnswerAsTheTableDoes`,
  `auth.TestSecurityRequirement_AnUnresolvedNameNamesADeclaredNearMiss`,
  `auth.TestSecurityRequirement_EveryErrorForANameNamesItsNearMiss`,
  `auth.TestNearest_TakesTheFirstReadingThatMatches`,
  `auth.TestNearest_ReadsOneEditWithinItsBounds`, `auth.TestOneEditApart_CountsOneEdit`,
  `service.TestFirstOccurrences_KeepsEachEntryOnceAtItsFirst`,
  `auth.TestAuth_RequirementsOrOfAnds`

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
  `servers.TestServerVariables_AnEmptyDefaultIsNotNone`,
  `e2e.TestCompile_AListInAnotherDocumentIsCheckedWhereItIsWritten`,
  `e2e.TestCompile_AServersListAtAPathItemLandingIsCheckedForRepeats`

## 15. Vendor extensions and policy

### `vendor-extensions`

An `x-*` key is legal on every object, so it is read at every object that admits one and kept
verbatim under `vendor_extension` ([`verbatim-preservation`](#verbatim-preservation)), with no
diagnostic. An object that lowers to a node with an `Unmodeled` map of its own keeps its extensions
there, unscoped. One that lowers to no node of its own is keyed by the path from its carrier down to
it, since several such objects reach one map and an unscoped key would leave the survivor to
lowering order ([unmodeled](../ir/unmodeled.md#keys)): on the Document, `openapi:info/x-*`,
`openapi:info/contact/x-*`, `openapi:info/license/x-*`, `openapi:externalDocs/x-*` and
`openapi:components/x-*`; on a tag's `TagDef`, `openapi:externalDocs/x-*`
([`tag-groups`](#tag-groups)); on the service, `openapi:paths/x-*`; on each operation, the path
item's `openapi:pathItem/x-*`, and `openapi:responses/x-*` and `openapi:externalDocs/x-*`; on the
HTTP binding, `openapi:callbacks/<name>/x-*`; on a content, `openapi:encoding/<part>/x-*` and
`openapi:itemEncoding/x-*`; on a schema's node, `openapi:xml/x-*`, `openapi:discriminator/x-*` and
`openapi:externalDocs/x-*`, a discriminator an `allOf` branch writes beside the union the position
lowers keeping its own on the `Union` holding it, `openapi:allOf/<n>/discriminator/x-*` at the
position and `openapi:discriminator/x-*` on one minted
([`union-beside-structure`](#union-beside-structure)); and on a security scheme,
`openapi:flows/x-*`. `<part>` and `<name>` are
escaped as RFC 6901 tokens, so a name the document chooses stays one segment
([unmodeled](../ir/unmodeled.md#keys)). A Link Object lowers to a link that holds its own, and one
kept verbatim keeps them inside its `openapi:links/<name>` entry ([`links`](#links)), as an encoding
entry naming no part keeps its own inside its `openapi:encoding/<key>` entry
([`multipart-encoding`](#multipart-encoding)); a path item that mounts no operation keeps its own
under `openapi:pathItem/<item pointer>/x-*` ([`path-item-servers`](#path-item-servers)). A key an
extension's value writes twice is kept at the copy the model reads and warned of once
([`verbatim-preservation`](#verbatim-preservation)). An extension whose value converts to no JSON
reaches the IR in no form, reported as an `openapi/degraded-construct` warning at the object
holding it.

- Cases: `extensions-x`, `inline-annotations`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperation_UnserializableExtensionStillWarns`,
  `operation.TestEncoding_PartNameWithSlashKeepsItsOwnKey`
- Status: partly built: an object kept verbatim also keeps its extensions under a scoped key (#849).

### `unknown-keys`

A key an OpenAPI object neither defines at any minor nor admits as an `x-` extension is reported by
the rule table ([`validation`](#validation)) as an `openapi/unknown-object-key` warning at the key,
since the object's key set is closed and such a key is usually a misspelling of its neighbour. The
table walks every object the grammar reaches, lowered or not, so a key is reported once wherever its
object is written: in a component nothing references, an encoding nested in another or kept by
position, a kept link, a schema's `xml`, `externalDocs` or `discriminator`, a schema kept verbatim,
and beside a Reference Object's `$ref`, which defines only `$ref`, `summary` and `description`. A
key merged in through `<<` is placed where it is written, as the table places every field. A key a
later minor defines draws the later version's warning instead
([`later-version-construct`](#later-version-construct)), and a Schema Object's keywords are its
dialect's, below. The lowering keeps the key verbatim under `openapi:<key>` in the `Unmodeled` of the
node that carries the object (`out_of_scope`) and reports nothing; an object that lowers to no node
of its own is keyed beneath the scope its extensions use (`openapi:info/<key>`,
`openapi:pathItem/<key>`, [`vendor-extensions`](#vendor-extensions)). A path item's is judged like
any other, a value carrying a YAML anchor included. A schema keyword no field of the schema model names is kept the same way,
reported as an info `openapi/unknown-schema-keyword`, because JSON Schema obliges an implementation
to ignore a keyword it does not know, and a position that lowers to a shared node and carries no
property or parameter hoists an alias for it to land on; `$comment`, `$dynamicAnchor` and
`$dynamicRef`, and 2019-09's `$recursiveAnchor` and `$recursiveRef` where the schema's dialect
defines them, are decided elsewhere and left out. A keyword the model names and the dialect the
schema is read in does not define, which only `nullable` is, where the nullable-inference policy is
off or does not read its value ([`nullable`](#nullable)), is kept the same way. One object contributes at most `annotation.MaxUnknownKeys` keys,
taken in byte order after the keys another reader already kept at the same pointer are set aside,
and the rest reach the IR in no form, reported as one `openapi/unknown-key-budget` warning at the
object.
Two other outcomes also keep nothing and warn: `openapi/unknown-key-entry-taken`, where
`openapi:<key>` is already held by a construct written elsewhere on a carrier two objects share, a
parameter's own key and its schema's keyword, whichever lowered first surviving; and
`openapi/unknown-key-unreachable`, where the key is merged in through a `<<` chain deeper than the
compiler's reader expands, so it reads no pair for it. A key merged in within that depth is read and
kept as a written one is. An object a rule keeps whole without reading it, such as an encoding
entry naming no part ([`multipart-encoding`](#multipart-encoding)), keeps its unknown keys inside
its entry, and the table reports each.

- Cases: `path-item-anchored-key`, `unknown-keywords-at-shared-nodes`
- Codes: `openapi/unknown-object-key`, `openapi/unknown-schema-keyword`,
  `openapi/unknown-key-budget`, `openapi/unknown-key-entry-taken`, `openapi/unknown-key-unreachable`
- Tests: `e2e.TestUnknownKeys_KeptAtEveryObject`,
  `e2e.TestUnknownKeys_SchemaAndObjectAreGradedApart`,
  `e2e.TestUnknownKeys_UnreferencedComponents`, `e2e.TestUnknownKeys_ObjectsKeptWhole`,
  `e2e.TestUnknownKeys_InsideAKeptSchema`, `e2e.TestUnknownKeys_ReportedOnceInEveryObjectAndMinor`,
  `e2e.TestUnknownKeys_MergedKeyIsPlacedWhereItIsWritten`,
  `e2e.TestUnknownKeys_SubObjectKeyMergedPastTheBoundIsReported`,
  `e2e.TestUnknownKeys_EveryKeptKeyIsReported`, `oas.TestCheck_UnknownKeysAreReportedAtEveryObject`,
  `oas.TestCheck_KeysBesideARef`,
  `annotation.TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes`,
  `annotation.TestUnknownKeywordsIn_AKeywordMergedPastTheBoundIsUnreachable`,
  `annotation.TestUndefinedKeywordsIn_KeepsWhatTheCallerNames`,
  `annotation.TestWritesUnknownKeyword_AsksWhatTheCensusWouldKeep`,
  `e2e.TestNullableInference_DisabledKeepsItAtASharedNode`
- Status: partly built: a parameter's key and its schema's keyword of one spelling share an entry
  (#396).

### `policies`

A fact the compiler fills by policy rather than reads off a keyword (invariant 6) is filled under
one model, which each policy's rule cites rather than restates: a promotion's
([`extension-promotion`](#extension-promotion)) and each inference's, as
[`discriminator-inference`](#discriminator-inference) and
[`idempotency-inference`](#idempotency-inference) are. Grouping
([`tag-groups`](#tag-groups), [`extension-groups`](#extension-groups)) is outside it, since it picks
how operations are arranged rather than filling a fact a node holds.

1. **Injection.** Each policy is one field of `Options`, a struct whose zero value is the policy
   switched on and whose `Disabled` switches it off, with a data member, such as the promotion's
   `Targets` or a list of media types, only where a caller sets one. Each policy also has a text
   option, a row of the compiler's one table of switches, spelled as the marker the policy leaves,
   which `false` turns off.
2. **Marking.** A fact is of one of three classes, which the `Inferred` list beside it tells apart
   ([promotion](../ir/unmodeled.md#promotion)): declared by a keyword, with no marker; promoted from
   an extension the document writes, marked `extension-promotion` on the node keeping the
   extension; or inferred from a convention, marked on the node holding it by a name for the fact
   in the form `<fact>-inference`, as `secret-inference` is. A flag a fact carries of its own, as
   `Pagination.Inferred` and `Discriminator.Inferred` are, says only that no keyword declared it,
   and the list says which of the other two classes filled it. A name is no such fact: the
   title-names policy marks no hint it gives, since a `Hint` with no `Source` is a suggestion
   already ([`naming`](#naming)), so its text option, `title-names`, spells the policy's name.
3. **Precedence.** Where several state a fact, a keyword decides first, then a promotion, then an
   inference, as a kind a conjunct declares decides before the one kind inference reads off
   another's shape keywords ([`shape-applicator`](#shape-applicator)); what a keyword's value states
   beside a promotion's key is the promotion's rule, as
   [`extension-promotion`](#extension-promotion) says of a `deprecated: false`. To an inference, the
   document has spoken wherever it writes a keyword, or a key a promotion maps to the fact's target,
   whatever its value: a value filled, one that failed to resolve, and an explicit `false` each keep
   the inference from filling that fact.
4. **Reports.** An inference filling a field the document leaves silent reports nothing. One whose
   reading replaces what the document writes, as an HTTP-date replaces the RFC 3339 text a written
   `date-time` names, reports an info `openapi/inferred-<fact>`. One whose trigger, the evidence its
   rule reads, is met where the document has not spoken, and which then declines to fill the fact,
   reports an info `openapi/undecided-<fact>` saying why.
5. **Placement and order.** A fact read once the document is whole is read in a package named for
   the fact or for the convention, which imports only `ir`, the compiler's `diag`, `ids` and
   `promotion`, and `whole`, the one reading of the finished document every such step shares, and
   `runtimeexpr` where it reads an expression the document writes, and so nothing of the walks that
   built the document. The compiler's run reads them in one block, in one order: links
   ([`links`](#links)), then the document's root, whose kept keys a promotion may read, then
   promotions, then inferences, so an inference sees what every promotion filled. Nothing after the
   root follows a reference, as which components the root keeps turns on what was reached. The
   audits of what the policies read come last, once the document's own keys are merged in as
   well. A reading a node needs while it lowers, as nullable inference's
   ([`nullable`](#nullable)) and kind inference's ([`shape-applicator`](#shape-applicator)), or while
   the registry settles, as discriminator inference's, is made there instead.

- Tests: `openapi.TestSwitchOptions_NameEveryPolicy`, `openapi.TestDecodeOptions_TextReachesEveryField`,
  `promotion.TestPromoter_WritesSeesAKeyWhateverItSays`, `promotion.TestPromoter_WritesKnowsEveryTarget`,
  `promotion.TestPromoter_WritesSeesEveryDefaultKey`,
  `pagination.TestInferWalks_LeavesWhatAnExtensionSpeaksOf`,
  `replacement.TestMessage_YieldsToWhatTheDocumentSays`, `convention.TestRedact_LeavesWhatTheDocumentSays`,
  `convention.TestIdempotency_ReportsWhatItCannotSettle`, `e2e.TestConventions_DisabledFillsNothing`,
  `openapi.TestSwitchOptions_EveryNameIsAMarker`, `openapi.TestDecodeOptions_RefusesARetiredName`,
  `e2e.TestMarkers_NameTheirOptions`

### `extension-promotion`

A vendor extension the promotion policy, `Options.Promotions` and the text option
`extension-promotion` ([`policies`](#policies)), maps to a typed field is read into that field as
well as kept ([unmodeled](../ir/unmodeled.md#promotion) states the four rules a promotion keeps).
Its `Targets` replace the default mapping. A target reads a list of sources, each an extension key
and, where the key's value is an object holding several facts, the member of it to read. The list
is the precedence among the sources: the first source stating a value fills the field, and a later
one stating another is reported as an info `openapi/degraded-construct` at its own key. An absent
member states nothing, and a key with no members to read is reported the same way, once however many
sources read it. The node keeping the extension is marked as [`policies`](#policies) marks a
promotion, as is a use its `$ref`'s target lends a field to, and the extension stays in `Unmodeled`
untouched.

A carrier also reads the objects whose keys it keeps beneath a scope: an operation its path item
(`openapi:pathItem/`), and a parameter or a header the content entry it elects (`content/<mt>/`) and
the schema of its spelling (`schema/` or `content/<mt>/schema/`). It reads them two ways, as
[annotations](../ir/annotations.md#what-a-reference-carries-down) reads a `$ref` target. A
single-sided fact, a redaction or a deprecation, holds where the carrier or any object it carries
states it, so a carrier's `x-sensitive: false` withdraws nothing its schema states, as it withdraws
nothing a `$ref` target states, and a deprecation's members fill from the nearest object stating
them. A valued fact, an operation's tier, a parameter's `allowReserved` or its pagination mark, is
the nearest statement's, the carrier's own first. The entry keeps its own provenance, so a finding
about a carried key sits at that key, and the carrier keeping it is the node marked. A misfit behind
a nearer statement of its key, which no reading takes, is still reported once at its own key, for a
redaction, a deprecation and an operation's tier; a parameter's `allowReserved` and its pagination
mark report only the nearest.

A source of a target stating that something is so, a deprecation, an open enum or union, a redaction
or a part of a page walk, may list the values that state it. Each is compared with a string's text,
or with a boolean's or a number's JSON, so `true` lists both `true` and `"true"`. A value it lists
states the fact, and any other states nothing. That other value is reported as an info
`openapi/degraded-construct` at the key, unless another source, of the same target or another, reads
it off the same member of the key, listing it or listing no values, whose reading then decides what
it is; a value every reader's list leaves out is reported by each target listing values there, as
none reads it. A key spelling openness `allow` or `disallow`, listed `allow`, opens an enum on
`allow` alone and reports `disallow` as it reports a misspelt `alow`, while the default's
`x-stability`, listed `deprecated` for a deprecation, leaves a `true` to the stability reading of
the same key, which reports it. A source with no list reads its target's own vocabulary, as below. A
key a pagination target reads is judged and reported as one statement of a page walk instead
([`extension-pagination`](#extension-pagination)).

A policy that would read less than it says is refused when the compile starts, as a Go error and no
diagnostic, since the mistake is the caller's and not the document's: a target outside the
vocabulary, a source whose key does not start `x-`, a list of values for a target reading a text, a
name or a tier, an owned prefix that is not `x-` followed by the start of a name, and targets or
prefixes beside `Disabled`. Beside the switch, a caller holding only text sets the mapping with the
option `promotion-targets`, the JSON object `Targets` encodes as, and the owned prefixes with
`promotion-prefixes`, separated by commas. The `extension-promotion` marker each node lists is the
record of what a run promoted.

The policy may own extension name prefixes, `Prefixes`, none by default. An extension under one
that the compiler reads no key of is reported as a warning `openapi/unread-extension` where it is
written, naming the key it reads nearest it by edit distance, as the likeliest one it misspells:
owning `x-deprecated` reports `x-deprecated-reasn` and names `x-deprecated-reason`. The keys read are
each reader's own, the mapping's sources and, where grouping is by extension, the key and the
aliases key [`extension-groups`](#extension-groups) reads, never a second list of them, so owning
`x-forge` beside the default grouping by extension reports no `x-forge-aliases`. Each node's
`Unmodeled` is read once the document is whole, under whatever scope its keys are kept; an
extension inside an entry kept verbatim, such as an `allOf` branch, is a value of that entry and is
not read.

`Deprecation` is filled on an operation, a parameter, a header, a property, a type node or a
security scheme. A key can state the deprecation itself: the default reads `x-fern-availability`,
then `x-stability`, and lists their word `deprecated` as the one value creating the `Deprecation`,
whatever a `deprecated: false` beside it says, since a `false` states nothing
([`ref-use-site`](#ref-use-site)). Any other word, a tier among them, states nothing, and so does a
boolean, which names no tier either and is reported once by the stability reading below. A key
listing no values reads the text `deprecated` and a boolean `true` as stating it, `false` and any
other word as stating nothing, and reports any other shape. A member is filled on what is deprecated
by then, by a keyword, a key, or a schema it composes as its `Base` whose keyword or key deprecates
it, as a decorator over a deprecated target is ([annotations](../ir/annotations.md#decorators)), and on
anything else the key annotates nothing and stays where it is. `Message` reads
`x-deprecated-reason`, `x-stainless-deprecation-message` and the `description` of
`x-cfDeprecation`; `Since` reads `x-deprecated-since`; and `RemovalDate` reads `x-sunset`, the `date`
of `x-forge-sunset` and the `eol` of `x-cfDeprecation`.

A property, header or parameter naming a `$ref` reads the target's mapped keys beneath its own, as
[`ref-use-site`](#ref-use-site) merges the target's other annotations: a key beside the `$ref` wins
field by field and without a report, and the target is the end of the chain, so a schema between the
two lends nothing, as it lends no keyword. One holding a refinement
([`allof-refinement`](#allof-refinement)), or a `$ref` to one, reads along the chain its keywords
are read along, nearest first: any link stating a deprecation, by a keyword or a key, deprecates the
use, the nearest deprecated link lends its members, and any link asking for redaction asks it of the
use. A node a `$ref` still speaks on once a value set takes its type reads that `$ref`'s keys the
same way, as it reads its keyword. A key on the target stating a deprecation counts as the target's
`deprecated`, which no `deprecated: false` beside the `$ref` withdraws. A target lends a member only
where it is deprecated itself, by a keyword or a key: on a live target the member annotates
nothing, as it does anywhere, and a use deprecated on its own does not change that. So the target's
`Deprecation` is all a use reads of it, which is what the [fold](../emitters/refine/decorators.md) of
a 3.0 decorator over the same target reads, and what the target's key cannot hold is reported where
that key is written and read, once, with a use reading past it in silence.

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
where one reading could override what the other reports. No default source lists the values
stating openness, as no key spelling it by a word has become common: a mapping naming one lists
them, and reads any other value as closed and reports it.

`Server.Name` reads `x-fern-server-name` on a server its document names in no other way, as no
server below 3.2 can be named: a 3.2 `name` is the document's own and the key does not override it.
Text names the server, which then carries the name as a declared one does, and empty text names
nothing, leaving the hint its URL gives. `OAuthFlow.TokenEndpointAuthMethods` is read through the
target `oauthFlow.tokenEndpointAuthMethods`, from the flow its key is written on, a string naming
one method and a list of strings several. No default key names it, as no spelling has become
common, and a list holding empty text or a method twice, which no set of names holds, is reported
as any other shape is. The methods are taken as written, as the registry naming them is open. The
flow, which has no `Provenance`, lists the marker in its own `Inferred`.
`HTTPParamBinding.AllowReserved` is read through the target `parameter.allowReserved` on a
parameter writing no `allowReserved`, which decides for itself, `true` asking for it and `false`
asking nothing. No default key names it either: the keyword states it wherever 3.2 applies it, and
a document below 3.2 reserving it at a path names its own key. The parameter lists the marker, as a
consumer reaches its binding from it.

`Deprecation.ReplacedBy` is read through the target `deprecation.replacedBy`, which no default key
names either, since no spelling for a replacement has become common. A mapped key's string, or list
of strings, names each replacement as the document names an entity of the node's class: an operation
by its `operationId`, a parameter of the same operation as a link's key names one (`<in>.<name>` or
a name one parameter carries), a header by its name among the headers beside it, a response's or
those an encoding gives a part or a stream's items, in any case, a property by its name within its
model, a schema by its name under `components/schemas`, and a security scheme by its key. A property
whose schema lowers to a type of its own, such as an inline object or enum, is deprecated on that
type ([`docs-and-deprecation`](#docs-and-deprecation)), so a name there is read as a schema's. The
names are resolved once the document is whole, since a replacement may be declared after what it
replaces, and they fill the field all or none: a name that names nothing, names more than one
entity, or names the node itself, leaves the field unset and is reported as an info
`openapi/degraded-construct` at the key, as no entity is picked from several. As a deprecation's
other members, a replacement on a node that is not deprecated annotates nothing, and it is read from
the node's own keys alone, never lent by a `$ref`'s target, whose class is another.

`stream.terminator` and `stream.condition` fill a stream's `Terminator` and `Condition`
([`streaming-media`](#streaming-media)), and `operation.paramChoices` the choices among an
operation's parameters ([`mutually-exclusive-parameters`](#mutually-exclusive-parameters)). A key
any of them reads that states nothing it can fill, a value
of another shape among them, is reported as an info `openapi/unpromoted-extension` at the key,
saying why.

- Cases: `extension-promotion`, `extension-stability`, `extension-redaction`, `extension-server-name`, `carried-keys`
- Codes: `openapi/degraded-construct`, `openapi/unread-extension`, `openapi/unpromoted-extension`
- Tests: `promotion.TestPromoterDeprecation_ValueThatIsNotTextIsReported`,
  `promotion.TestShadowed_ReturnsWhatCarriedPassesOver`,
  `operation.TestPromotion_AMisfitBehindANearerKeyIsReported`,
  `promotion.TestPromoter_ValuesNameWhatStatesTheFact`,
  `promotion.TestPromoter_AValueAnotherTargetReadsIsItsToReport`,
  `promotion.TestPromoter_ACallersValuesAreCopied`,
  `promotion.TestPromoter_AReferentsKeyDeprecatesTheUse`,
  `promotion.TestPromoter_ADeprecatedBaseHomesTheNodesMembers`,
  `promotion.TestTargets_ListsEveryDeclaredTarget`,
  `promotion.TestExtensionPromotionsCheck_Refusals`,
  `promotion.TestExtensionPromotionsCheck_AcceptsWhatReadsAsWritten`,
  `promotion.TestListsValues_NamesTheTargetsStatingAFact`,
  `promotion.TestUnreadExtensions_ReportsAnOwnedKeyNoSourceReads`,
  `promotion.TestUnreadExtensions_NamesNoKeyAMappingDoesNotRead`,
  `promotion.TestUnreadExtensions_TheGroupingKeyIsRead`,
  `promotion.TestUnreadExtensions_TheAliasesKeyIsRead`,
  `service.TestGroupingExtension_KeysAreWhatGroupingReads`,
  `e2e.TestPromotion_AnOwnedPrefixIsJudgedAtEveryNode`,
  `e2e.TestPromotion_AnOwnedPrefixCountsEveryGroupingKeyRead`,
  `promotion.TestServerName_ReadsTheDefaultKey`,
  `promotion.TestTokenEndpointAuthMethods_ReadsASet`,
  `promotion.TestAllowReserved_OnlyTrueAsksForIt`,
  `operation.TestLowerParameter_AKeyAsksForReservedExpansion`,
  `servers.TestLowerServer_AKeyNamesAServerTheDocumentNamesNot`,
  `auth.TestLowerSecuritySchemes_AMappedKeyStatesTheTokenEndpointsAuthentication`,
  `schema.TestUse_ADecoratorOverADeprecatedBaseReadsItsMembers`,
  `openapi.TestCompile_RefusesAPromotionPolicyItCannotRead`,
  `openapi.TestDecodeOptions_TextReachesEveryField`,
  `promotion.TestPromoterDeprecation_KeyStatesIt`,
  `promotion.TestStability_ReadsEachWord`,
  `promotion.TestPromoterRedaction_OnlyTrueWrites`,
  `promotion.TestEnumOpenness_WritesNothing`,
  `promotion.TestUnionOpenness_OpensOnAMappedKey`,
  `e2e.TestPromotion_AMappedKeyOpensAUnion`,
  `e2e.TestPromotion_AMixedEnumsUnionReadsTheEnumTargetAlone`,
  `promotion.TestPromoter_ReadsSourcesInPolicyOrder`,
  `promotion.TestPromoter_MemberReadsOneFactOfAnObject`,
  `promotion.TestPromoter_ReferentLendsBeneathOwnKeys`,
  `promotion.TestPromoter_ReferentReadsWhatASchemaLends`,
  `promotion.TestExtensionTarget_EveryTargetIsInThePromotionTable`,
  `promotion.TestPromoter_ReadsReplacementNames`, `whole.TestParamIndex_NamedReadsALocationQualifiedName`,
  `replacement.TestResolve_NamesEachReplacementInItsNodesScope`,
  `replacement.TestResolve_ANameNamingNothingFillsNothing`,
  `replacement.TestResolve_ANameNamingSeveralFillsNothing`,
  `e2e.TestPromotion_ReplacementsResolveOnceTheDocumentIsWhole`,
  `e2e.TestPromotion_RemovalDateAndVersionAreSeparateFacts`,
  `e2e.TestPromotion_AUseReadsItsReferentsKeys`,
  `e2e.TestPromotion_ARefinementLendsWhatItRefines`,
  `e2e.TestPromotion_ALostRefLendsTheNodeItSpeaksOn`,
  `e2e.TestPromotion_ADecoratorMatchesTheRefItStandsFor`,
  `e2e.TestPromotion_UsesHoldTheirOwnAnswerInEitherOrder`,
  `e2e.TestPromotion_AContentCarrierReadsTheEntryItElects`, `promotion.TestCarried_ReturnsOwnWhereNothingIsCarried`,
  `promotion.TestCarried_TheOwnKeyBeatsTheScopedOne`, `promotion.TestLent_ASingleSidedFactHoldsWhereAnyStatesIt`,
  `e2e.TestCarriedKeys_AnOperationReadsItsPathItemsTier`
- Status: partly built: a redaction key beside an `allOf` over an
  object lands on the decorator the `allOf` lowers to, whose `Sensitive` keeps it a type of its own,
  where beside a `$ref` it lands on the carrier (#1220); a schema a use names
  between it and the end of its chain lends no key, as it lends no keyword (#1029); and a key inside
  a `oneOf` kept verbatim beside a body, as the base of a hierarchy keeps the union of its subtypes
  ([`union-beside-structure`](#union-beside-structure)), reaches no field.

### `mutually-exclusive-parameters`

The promotion target `operation.paramChoices` ([`extension-promotion`](#extension-promotion)),
read by default from `x-mutually-exclusive-parameter-groups`, fills `Operation.ParamChoices`
([choices among parameters](../ir/operations.md#choices-among-parameters)) once the document is
whole. The key holds the convention's object, from a choice's name to
`{optional: <boolean>, variants: {<variant's name>: [<parameter name>, …]}}`, read whole; `Required`
is `!optional`. A parameter name is resolved as a link's `parameters` key is, `<in>.<name>` or a
name one parameter carries, among the operation's parameters, a path item's included. Choices and
variants come out sorted by name, whatever order the document writes them in, and a variant's
parameters in its list's order. The operation's `Params`, its bindings and every reference to a
parameter are untouched, and no parameter or type is made. The first source whose key the operation
writes is read, whatever it holds; a later source's key writing the same value is silent, and one
writing another is reported and not read. The key stays
verbatim, `vendor_extension`, and the operation lists `extension-promotion` in its
`Provenance.Inferred` where a choice is filled.

Each choice is filled whole or not at all, and one written badly leaves the others standing. Each
refusal is one info `openapi/unpromoted-extension`, at the most specific coordinate:

| The document writes | Reported at |
|---|---|
| a key value that is no object, a list of lists among them | the key |
| a choice that is no object | `<key>/<choice>` |
| no `optional`, or one that is no boolean | `<key>/<choice>`, or `…/optional` |
| no `variants`, one that is no object, or fewer than two variants | `<key>/<choice>`, or `…/variants` |
| a variant that is no list of strings, or an empty one | `…/variants/<variant>` |
| a name naming no parameter, or several | `…/variants/<variant>/<index>` |
| a name twice in one variant | `…/variants/<variant>/<index>`, the later |
| a parameter every request carries that some variant lacks | `<key>/<choice>` |

The last is refused because the choice would be false: a request picking another variant carries
the required parameter. Alternatives among a request body's members are out of scope: a schema
states them with `oneOf`, and they lower as the body's type, so the convention's body key is not
read.

- Cases: `param-choices`
- Codes: `openapi/unpromoted-extension`
- Tests: `e2e.TestParamChoices_FillsChoices`, `e2e.TestParamChoices_ChangesNothingElse`,
  `e2e.TestParamChoices_SourceOrderDecidesNothing`, `e2e.TestParamChoices_LeavesAChoiceItCannotRead`,
  `e2e.TestParamChoices_ReadsNothingOfAKeyThatIsNoObject`, `e2e.TestParamChoices_FollowsThePolicy`,
  `e2e.TestParamChoices_ResolvesAPathItemsParameter`,
  `e2e.TestParamChoices_ConformanceReadsTheSameInEitherOrder`,
  `paramchoice.TestPromote_ReadsEachChoiceWholeOrNotAtAll`,
  `paramchoice.TestPromote_FillsAChoiceEveryVariantOfWhichCarriesARequiredParameter`,
  `paramchoice.TestPromote_ReadsNoKeyThatIsNoObject`,
  `paramchoice.TestPromote_ReadsNothingWhereNoKeyIsWritten`,
  `promotion.TestStatementOf_ReadsTheFirstSourceWritten`

### `extension-pagination`

A page walk an extension declares fills `Operation.Pagination`
([extension-promotion](#extension-promotion)), read once the document is whole, since its values are
members of response bodies. Each field is a target of the policy: `pagination.cursor`,
`pagination.offset` and `pagination.page` the input continuing a walk of strategy `cursor`, `offset`
or `page`, `pagination.limit` the page size, and `pagination.items`, `pagination.nextCursor`,
`pagination.nextLink`, `pagination.totalCount` and `pagination.totalPages` the page values of the
same names, and `pagination.hasMore` the flag a page states that another follows, which fills
`HasMore` and ends the walk by it alone, `Stop: [no_more]`, and which no default source reads. A
source listing no values holds, on the operation, an expression naming the value: an
OpenAPI runtime expression, `$request.query.<name>`, `$request.path.<name>`,
`$request.header.<name>` or `$request.body` and a pointer for an input, and `$response.body` and a
pointer or `$response.header.<name>` for a page value, or the shorthand `$request.` or `$response.`
and a dotted chain of names, the first of an input a parameter's where one parameter carries it and
the request body's member otherwise. A source listing values marks the node its key sits on as the
value its word names: a parameter, a property, a response header, or the type one of them names,
found from the request, its parameters and its body, or from each success response's headers and
body, nearest the body's root. The default reads both conventions: the `cursor`, `next_cursor`,
`results` and `next_uri` members of `x-fern-pagination`, and the `purpose` of
`x-stainless-pagination-property`, whose `next_cursor_param`, `next_cursor_field` and `items` mark
the cursor, the next cursor and the items. Neither reads an offset or a page number by default,
since each convention leaves to its own configuration which of the two its walk counts.

A page value is one member of every success response declaring a body, of each of its media types,
and a header one of every success response, whether an expression or a mark names it, as a pager
reads whichever arrives. A media type that streams
([payloads](../ir/payloads.md#a-content-that-streams)) is a sequence of frames and no page, so it is
not read, and a response every media type of which streams declares no page body, as one declaring
no body does not. A value that a response or a media type holds at another member, or not at all, as
a header two success responses each declare does, resolves to nothing, as does a value a mark finds
at two members as near the root, or at a header and a member of one response. A body or a member
typed as an alias of a model is read as that model, as a link reads one ([`links`](#links)). The
fields fill a pagination whole or not at all: a value that resolves to nothing, two inputs
continuing the walk, a cursor beside no next cursor or a next cursor beside no cursor, an offset or
a page number beside no items, which a pager counts to advance an offset and to end either walk
([pagination](../emitters/operations/pagination.md#what-a-pager-sends-next)), and a walk with
neither an input nor a next link are each reported as an info `openapi/degraded-construct`, at the
value or at the operation, and leave the field unset. The pagination sets its own `Inferred` and
states no `Stop`, which no extension of either convention states, and each node whose key it read
lists the marker `extension-promotion`. A later source of a target naming another value than the
first is reported and loses, as for every target. An operation holding a pagination already is not
read.

What a key holds is one statement to every source of a pagination target reading the key the same
way, as an expression or as a mark, and is one of three things. It states part of a walk where it is
an object holding a member a source reads, or, for a source reading the value whole, that value: an
expression, written as a string, or a word a source lists, each read as above. `false` states that
no walk, or no value of one, is there: nothing is read from it and nothing is reported. Any other
value states nothing a source can read: `true` beyond the operation, null, a string, a number or a
list where the sources read a member; an object holding none of the members they read, such as
`{}`; an expression that is no string or is empty; and a word none of the sources of its member
lists. Each is reported as an info `openapi/degraded-construct` at the key, or at the member read,
once however many sources read it, saying that the key, being written, keeps an inferred pagination
off where it is read. A word one source lists is reported by none of the others, even where only a
source reading the other side of the walk lists it.

An operation writing a key `true`, where its sources read a member, reads the walk the document's
root states under the same key, as though the operation had written the root's object; the
operation keeps its `true` and lists the marker where a pagination is filled, and the root's entry
stays kept, no field of the `Document` being filled from it. This is the rule of every
member-reading pagination source, not a field of the source. A root writing no such key, or a value
that is no object, a root `true` being followed no further, states no walk, and the operation is
stated all the same: the former is reported as an info `openapi/unpromoted-extension` at the
operation's key, the latter at the root's key, so every operation writing `true` shares one finding.
A `true` beside the same key its path item writes, which the operation keeps as
`openapi:pathItem/<key>`, reads the path item's statement before the root's: it is the nearer one,
and the root's is not taken in its place, so a path item's value that is no object is reported at
its own key. A statement is never carried otherwise: an operation writing nothing has not opted in,
whatever its path item or the root states, and its inference is unchanged.

A pagination is read in the precedence [`policies`](#policies) gives, so no inference
([`pagination-policy`](#pagination-policy)) fills one the document spoke of. OpenAPI writes no
pagination keyword, so the document has spoken of an operation's pagination wherever it writes a key
a source of a pagination target names, where this rule reads that source: on the operation for an
expression, and for a mark on a parameter, a member of the request's body or of a page's, a success
response's header or the type one of them names, however many operations reach it. A value filling a
pagination, one resolving to nothing or refused, `false`, and one stating nothing a source can read
speak alike. `pagination.Promote` returns the operations it reads that so speak, by ID, as the set a
step inferring a pagination reads in place of their extensions, and marks no node for a pagination
it does not fill. A key written anywhere else, such as on an error response, in a media type that
streams, or as a mark on the operation itself, is read by no source, so it speaks of nothing.

- Cases: `extension-pagination`, `body-alias`, `extension-pagination-root`
- Codes: `openapi/degraded-construct`, `openapi/unpromoted-extension`
- Tests: `pagination.TestPromote_FillsWhatTheExtensionsDeclare`,
  `pagination.TestPromote_ReportsWhatFillsNone`,
  `pagination.TestPromote_ReadsEverySuccessResponse`,
  `pagination.TestPromote_ReadsNothingOffPolicy`,
  `pagination.TestPromote_SaysWhichOperationsSpeakOfAPagination`,
  `pagination.TestPromote_ReportsEachValueAKeyHolds`,
  `pagination.TestPromote_StatedReadsEitherSidesKey`,
  `pagination.TestPromote_StatedIsIndependentOfDeclarationOrder`,
  `pagination.TestPromote_StatesNothingOffPolicy`,
  `pagination.TestPromote_ReadsNoOperationHoldingOne`,
  `pagination.TestPromote_ReadsAHeaderOffItsOwnOperation`,
  `pagination.TestPromote_AllocatesLinearInWhatItReads`,
  `pagination.TestPromote_ReadsAPageThroughAnAlias`,
  `whole.TestDocument_APromotionNoMemberMarksFlattensNoModel`,
  `whole.TestDocument_ListsEveryOperationOnce`,
  `pagination.TestMarker_AUseOfATypeTheRegistryLacksMarksNothing`,
  `promotion.TestPageStatementOf_ReadsAnExpressionOrAMark`,
  `promotion.TestPageStatementOf_ReportsWhatStatesNoValueOfAWalk`,
  `pagination.TestInferWalks_LeavesWhatAnExtensionSpeaksOf`, `pagination.TestPromote_FillsTheFlag`,
  `pagination.TestPromote_ReadsTheRootForTrue`, `pagination.TestPromote_TrueWithNoRootStatement`,
  `promotion.TestPageStatementOf_ReadsTheRootForTrue`, `pagination.TestPromote_TrueReadsThePathItemFirst`,
  `pagination.TestPromote_ASilentOperationReadsNeitherItsPathItemNorTheRoot`

### `pagination-policy`

OpenAPI has no keyword for pagination, so where no extension speaks of an operation's walk, the
operation states it only by the names it uses, which the pagination-inference policy,
`Options.PaginationInference` and the text option `pagination-inference` ([`policies`](#policies)),
reads. It reads each operation the client sends, a webhook or a callback being one it receives, once
the registry is whole, and yields as [`policies`](#policies) orders every inference: an operation
holding a `Pagination`, or one whose extensions speak of a pagination where
[`extension-pagination`](#extension-pagination) reads them, is neither read nor reported, as the
document has answered for it. Under a promotion policy mapping no pagination target,
`extension-promotion=false` among them, no key is read, so no extension speaks and the policy reads
every operation. A name is compared by its canonical words (`Naming.Canonical`), so `pageSize` and
`page_size` are one name:

1. **Where a walk continues** is a place named `page` or `page_number`, under the `page` strategy;
   `offset`, under `offset`; `cursor`, under `cursor`; or `page_token`, `next_token` or
   `continuation_token`, under `token`. It is `InputCursor`, and a place named `per_page`,
   `page_size` or `limit` beside it is `InputLimit`. A place is a query parameter; a member of an
   object query parameter, one bound in the query or as the query string whose type stands for a
   model, one level down, an `InputPath` naming the parameter and the member; or a member of the
   request body's root that every content holds, an `InputPath` naming the member alone. An object
   parameter is read through its members: where one of them names a continuation or the page size,
   its own name gives it no role, so a JSON:API `page` holding `offset` and `limit` is one `offset`
   walk, and a `page` or a `filter` holding only `limit` beside a `cursor` parameter is that
   cursor's walk with the member its page size; where none does, its name is read as any
   parameter's. Each place is a value a walk sends or counts
   with: its type, read through alias scalars, is a plain number of any kind or a plain string, the
   text a number is sent as, and not an enum, a constant or an encoded string, whatever values it
   holds. A `page` sent as a string is read only where the default or an example of its parameter or
   member is a whole number, as some APIs name an opaque page token `page` too. A name such as
   `after`, `marker` or `starting_after`, which names a cursor in one API and a filter or an ID in
   another, is not read. Two places naming a continuation, or two naming the page size, leave the
   walk undecided, each named.

   A walk sends its operation again for each page, so the method decides what is read. A `GET` or a
   `QUERY`, which asks for no change on the server, is read from any place. A `POST` is read where a
   member of its body names a `page`, `cursor` or `token` continuation, or a query parameter names
   any, and settles only where all of these hold, each failing one reported with its own reason:
   where the walk continues is a member of the request body, since a `POST` taking its path item's
   page parameters, as one creating a resource does, would create it once per page; the strategy
   is `page`, `cursor` or `token`, a body `offset` being no trigger and drawing nothing, as it is
   often an identifier; a `page` walk names its page size in the body too; a `cursor` or `token`
   walk's next cursor is named as the cursor with `next` before it, never as the cursor itself, as
   a create answers with what it was sent; the items are the list named as items at the page's root,
   or the page is a list, the fallbacks of a model holding them or of the one list being left out;
   and every success response declaring a body is a `200`, as a `201` says a resource was created
   (RFC 9110 §15.3.2). Any other method, whose body states what to store, change or remove, is read
   as no walk, and one whose query parameters name a continuation is reported. Which declaration a
   body member takes where two `allOf` entries declare it follows their order, so a body composing
   `page` as an integer in one entry and a string in another is a walk in one order and undecided
   in the other.
2. **A page** is the body of each content of each success response, the `Default` aside, and each
   must hold what the walk reads at the same members, as a pager reads whichever arrives. One with
   no body, such as a `204` or a `304`, holds none and is not read, as a pager ends at it as at an
   empty page ([other responses](../emitters/plan/responses.md#other-responses)), and a content that
   streams ([payloads](../ir/payloads.md#a-content-that-streams)) is a sequence of frames and no
   page, so it is not read either. A path starts at
   the body whole. A list body holds the items whole, an `Items` with no segment, and no next
   cursor. A model's members are read across its composition (`ir.FlattenedProperties`), each
   as the declaration the flattening takes, which is the type a page decodes it as, and its items
   are the one list member named `result`, `results`, `data`, `items`, `records`, `entries` or
   `values`; where no list is so named, the one list the one model member so named holds; else the
   body's one list.
3. **The next cursor** of a `cursor` or a `token` walk is `NextCursor`: the one member of the body,
   or of a model a member of it holds, of the cursor's own primitive, named as the cursor with
   `next` before it, or, where no member is so named, as the cursor itself, since a page naming
   both echoes the cursor it was sent beside the next, or, for a cursor that is no string, `next`.
   A plain string `next` beside a string cursor may be the cursor or a link, so where it stands
   alone the walk is undecided. Where the body names none, the next cursor is the response header
   every success response declares once whose canonical name, a leading `x_` aside, is the
   cursor's with `next` before it, or the cursor's own, a `NextCursor` whose `In` is `header`; a
   next cursor the body and a header both name leaves the walk undecided. A `POST` reads no header
   next cursor, as its next cursor is the page's member named as the cursor with `next` before it
   (point 1). Such a walk lists
   `no_next` in `Stop`, as an empty next cursor names no page; a walk under `page` or `offset` lists
   none, a page holding no
   items ending it whatever `Stop` lists
   ([pagination](../emitters/operations/pagination.md#where-a-page-walk-ends)).
4. **The flag** of a `cursor` or a `token` walk is `HasMore`: the one boolean member of the body, or
   of a model a member of it holds, named `has_more`, `has_next`, `has_next_page` or
   `is_truncated`. The walk then ends by it and lists `no_more` alone, the page's own statement of
   where the walk ends, since a page may keep handing back a cursor past its last. Flags are counted
   by member, so one name at two places is two, and two flags leave the walk undecided, as taking
   the first met would end it at the wrong one and turn on declaration order. A boolean beside the
   next cursor under another name, a bare `truncated` among them, leaves it undecided too: it may
   say more follows or that a result was cut short for good, and a pager ending there would drop
   pages. A flag further down, or in a header, is not read, and a walk under `page` or `offset`
   reads none.
5. **The totals** are `TotalCount`, the one member named `total_count`, and `TotalPages`, the one
   named `total_pages`, each of a type a walk counts with, in the body or in a model a member of it
   holds. A page naming none or several states no total, and the field is unset.
6. **The links** are `NextLink`, the one member named `next_link` typed as a string or a `url`,
   else the one named `next` typed as a `url`, and `PrevLink`, the one named `prev_link` or
   `previous_link` typed as either, else the one named `prev` or `previous` typed as a `url`, in the
   body or in a model a member of it holds. Beside a walk an input continues they are facts of the
   page, which change neither its strategy nor what is reported; a link the contents hold at
   different members, or none or several of, is left unset, and the walk stands. `FirstLink`,
   `LastLink` and a flag for a link walk are not read.
7. **A link walk** is read where no place names a continuation, on a `GET` or a `QUERY`, and the
   response is the only evidence, so it must look like a page and nothing else: the root of each
   content holds one list named as items or `value`, the name the next-link convention pairs with
   `nextLink`; the links of point 6, at the root; and beyond those only whole-number counts named
   `count`, `total`, `total_count` or `total_pages`. Any other member, a `title` or an `id`, makes
   it a resource that links to its neighbour, and no walk; a link below the root, or a count that is
   no whole number, the same. Such a page stating a next link is a `next_link` walk: `NextLink`,
   `PrevLink` where stated, `Items`, the totals of point 5 and `Stop: [no_next]`. Such a page, or a
   list, stating none, where every success response declaring a body declares a `Link` header of a
   plain string type (RFC 8288), is a `link_header` walk, with `Items`, the totals and `Stop:
   [no_next]`, the strategy reading the header's `next` link. A response that states a link and is
   no page triggers nothing and draws no finding; beside an input that continues the walk, a `Link`
   header changes nothing.

What the policy fills is marked as [`policies`](#policies) marks an inference: `Pagination.Inferred`
is set, and the operation's `Provenance.Inferred` names `pagination-inference`. An operation whose
query parameters name where a walk continues and that the policy cannot settle gets no `Pagination`,
never a guess, reported as an info `openapi/undecided-pagination` at the operation's declaration
naming what stops it: a method other than `GET`, `QUERY` and `POST`, a condition a `POST` fails,
two places naming a continuation or a page size, one of a type a walk does not send, a `page` string
with no whole number for its
default or an example, no success response or none with a body, a body that is neither an object nor
a list, items or a next cursor it names none or several of, two flags, a boolean beside the next cursor
named as none, a next cursor the body and a header both name, a plain string `next` beside a string
cursor, a link walk's page stating a next link and a `Link` header, or bodies holding the page at
different members. A path item mounted twice is reported once, at the
declaration both mounts share.

- Cases: `pagination-inferred`, `pagination-flag`, `pagination-body`, `pagination-links`
- Codes: `openapi/undecided-pagination`
- Tests: `pagination.TestInferWalks_ReadsEachStrategy`,
  `pagination.TestInferWalks_MarksWhatItFills`,
  `pagination.TestInferWalks_LeavesWhatItCannotSettle`,
  `pagination.TestInferWalks_ReadsEveryResponse`,
  `pagination.TestInferWalks_LeavesAnOperationNamingNoWalk`,
  `pagination.TestInferWalks_ReadsOnlyAMethodSafeToSendAgain`,
  `pagination.TestInferWalks_KeepsAPaginationTheOperationHolds`,
  `pagination.TestInferWalks_LeavesWhatAnExtensionSpeaksOf`,
  `pagination.TestInferWalks_ReadsTheSameInEitherOrder`,
  `pagination.TestInferWalks_ReportsTheSameWhateverTheResponseOrder`,
  `pagination.TestInferWalks_ReadsAMountedDeclarationForEachMount`,
  `pagination.TestInferWalks_EndsACursorWalkAtItsFlag`, `pagination.TestInferWalks_FlagNames`,
  `pagination.TestInferWalks_ReadsNoFlagForAPageWalk`,
  `pagination.TestInferWalks_ReadsAPostThatSearches`,
  `pagination.TestInferWalks_LeavesAPostItCannotTellFromACreate`,
  `pagination.TestInferWalks_ReadsNoBodyOfAMethodThatIsNoSearch`,
  `pagination.TestInferWalks_ReadsAnObjectParameter`,
  `pagination.TestInferWalks_ReadsAnObjectParameterNamedForARole`,
  `pagination.TestInferWalks_ReadsAPageSizeAnObjectParameterHolds`,
  `pagination.TestInferWalks_TwoPlacesAreUndecided`, `pagination.TestInferWalks_ReadsLinks`,
  `pagination.TestInferWalks_ALinkHeldApartIsLeftUnset`, `pagination.TestInferWalks_NextNames`,
  `pagination.TestInferWalks_ReadsAHeaderCursor`, `pagination.TestInferWalks_LinkWalks`,
  `pagination.TestInferWalks_ReadsTheBodyMembersEveryContentHolds`,
  `pagination.TestInferWalks_SaysWhyAPostIsNoWalk`, `pagination.TestInferWalks_ReadsALinkWalkOnlyOffAPage`,
  `pagination.TestInferWalks_AllocatesLinearInTheBodiesItReads`,
  `whole.TestDocument_StandsReadsThroughAliasScalarsToTheCap`,
  `whole.TestDocument_ListsEveryOperationOnce`,
  `pagination.TestInference_TheZeroValueHonoursThePolicy`,
  `openapi.TestDecodeOptions_TextReachesEveryField`

## 16. Swagger 2.0

### `swagger-lift`

A Swagger 2.0 document is to be lifted to the OpenAPI 3.x shape first — `body` and `formData`
parameters to a `Payload`, `host`, `basePath` and `schemes` to `Servers`, `consumes` and `produces`
to content types — and then lowered by the rules above.

- Status: not built: a Swagger 2.0 document is recognized and refused with
  `engine/no-compiler-for-format` until milestone 2, and the lift is to keep provenance pointing
  into the 2.0 source (#681).
