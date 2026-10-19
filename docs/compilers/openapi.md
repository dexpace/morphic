# OpenAPI compiler — lowering specification

This document is normative for `compilers/openapi`, as [`ir-design.md`](../ir-design.md) is for
the IR: it states how a construct of an OpenAPI 3.0, 3.1 or 3.2 document lowers into the IR, which
diagnostic the compiler reports, and where it keeps what the IR does not model. The IR's shapes,
and what a consumer may rely on, are ir-design's, and nothing here restates them.

A bare section number (`§4.3`) names a section of ir-design. Each rule is a heading whose text is
its key, a stable slug that code, tests and the other documents cite as
`docs/compilers/openapi.md#<key>`. A rule keeps its key when its text changes, and a rule that is
renamed takes every citation of it along.

A rule closes with the evidence that pins it, one bullet per field:

- **Cases** — the conformance cases (`testdata/conformance/openapi`) that exercise it.
- **Codes** — every diagnostic code it reports, as `compilers/openapi/internal/diag` declares them.
  The text names a code where it says when the code fires or at which severity.
- **Tests** — Go tests that pin what no case reaches, as `package.TestName`: `openapi` for the
  compiler's own package, an internal package's name otherwise.
- **Status** — where the compiler does not yet do all the rule says, naming the issue that tracks
  it, or the milestone that schedules work not yet begun.

The fields are the only bullets a rule writes at the margin, since each is read as one; a list in a
rule's text is numbered.

A rule names a case or a test, or says in its Status why it has neither. The `TestLoweringSpec`
tests in `compilers/openapi` hold every field to the code and read it both ways: each conformance
case is named by a rule, and each code by a rule or by `codesOutsideTheRules`, which gives the
reason a code has no rule. [`CONTRIBUTING.md`](../../CONTRIBUTING.md) walks adding a rule.

## 1. Documents and versions

### `document-metadata`

info contact/license → Document

- Cases: `docs-summary-desc`
- Tests: `openapi.TestMeta_FullDocumentMetadata`, `openapi.TestCompile_EndToEnd`

### `self-uri`

`$self` → that source's `SourceInfo.SelfURI` as written, a relative one unresolved, and at every
minor, with a 3.0 or 3.1 document writing one warned `later-version-construct` as for every
construct a later minor added ([`later-version-construct`](#later-version-construct))

- Cases: `self-uri`
- Codes: `openapi/later-version-construct`
- Tests: `openapi.TestCompile_RecordsSelfURI`

### `later-version-construct`

**A construct a later minor adds.** A document may write a field, or one value of a field, that a
later minor of its format added to the object it sits on: a `name` on an OpenAPI 3.1 Server Object,
`in: querystring` on a 3.0 Parameter Object. It lowers exactly as in a document declaring the minor
that added it, and is reported once, at its own key, as `openapi/later-version-construct`. That
reading loses the least: from the lowering and the diagnostic a consumer can still set the construct
aside, while from a verbatim copy it could not rebuild the operation a `query` key declares. Except
in the one case set out below, the declared minor gives the construct no meaning at all, so the
lowering asserts nothing the document does not write. It is a warning, as `unknown-object-key` is —
an OpenAPI object's key set is closed, so the document is invalid for the version it declares, but
it still lowers — and a consumer that wants it refused has `--fail-on warning`. The rule covers
constructs a minor *added*, including the Reference Object's `summary` and `description`, which 3.1
added and which [`reference-object-siblings`](#reference-object-siblings) folds at every version.
That is the one case: 3.0 admits no key beside a `$ref` and says one SHALL be ignored, so a 3.0
document writing one is warned that it was read as 3.1 reads it. Beside a schema's `$ref` 3.1 admits
any keyword, which no row of the table expresses, so a 3.0 schema's `$ref` siblings are GitHub
#871's question. A keyword whose meaning differs between minors, such as `exclusiveMinimum`, follows
the declared one, and a keyword a later minor removed is another question (GitHub #632).

Whether a construct is a later minor's is asked in one place: the load phase holds every object the
source's model holds to one table of what each minor added, and no lowering asks it, so no field's
treatment can depend on whether the parser happens to model it. The keys beside a `$ref` are judged
as the object they belong to: a Reference Object's, or a Path Item's own fields beside its `$ref`.
Where the parser refuses such a construct itself — a Path Item's `additionalOperations` below 3.2, a
schema keyword the 3.1 meta-schema forbids — the compiler takes the refusal over, and only the
refusal: what the 3.2 meta-schema merely stopped requiring still binds the declared version. Where
refusing made the parser skip checking what the construct holds, as inside `additionalOperations`,
the compiler runs those checks itself. The table and the takeover both cover the source document: in
a document a reference reaches, the parser's own treatment stands, and the minor that governs such a
document is GitHub #839's to settle. An object a `$ref` reaches in the source outside the model,
such as under an `x-` key, is held to the table and the takeover as one the model holds is, and
reported at the pointers it is written at (GitHub #872). The fields beside a Path Item's `$ref` are
modelled and walked with the source, so they are held to both as the source's own objects are
(#577). The same pass, with the same reach, judges a Parameter Object's `allowReserved` against
where the declared minor applies it ([`allow-reserved-locations`](#allow-reserved-locations)). A
JSON Schema 2020-12 keyword in a 3.0 schema is GitHub
#820's.

- Cases: `later-version-constructs`, `reached-outside-model`
- Codes: `openapi/later-version-construct`, `openapi/validation`
- Tests: `openapi.TestLaterVersionConstructs_LowerAsInTheVersionThatAddedThem`,
  `load.TestAdditions_EachIsReportedBelowItsMinorOnly`,
  `load.TestMetaSchemaVersionArtifacts_OlderMinorsOnlyWhenLater`

### `minor-required-fields`

**What a minor requires.** The parser's REQUIRED-field rule is held to the declared version as well.
It requires at every minor two fields a later one stopped requiring — an Operation's `responses`
from 3.1, a Response's `description` from 3.2 — and it reads a field written as the empty string, or
as an empty map, as missing, where every minor's schema types the field as a bare string or map; its
server URL check repeats that reading for a variable's `default`. The load phase drops those
findings wherever the parser makes them: in the source, in an object a `$ref` reaches outside the
model, and in another document, which is held to the source's declared minor as its schemas'
findings are until GitHub #839 settles which minor governs it. Nothing else is dropped. An empty
value the specification, or a grammar it cites, refuses stands: an enumerated field (a Parameter's
`in`, a security scheme's `type`); a path parameter's `name`, which must match a template
expression, none of which the empty string names; a name sent in a header or a cookie, by a
parameter or an API key, and an HTTP `scheme`, each a token of RFC 9110 or RFC 6265; and a server
`url` from 3.2, whose server URL grammar requires a character. A name sent in the query, or a
`querystring` parameter's, which is never sent, is admitted. A field left out below the minor that
relaxed it stands too, as does a `null`, and so does every finding at a node two reads share through
a YAML alias or merge key, unless every read there draws one of those dropped there. The objects a
later minor's construct holds are held to the declared minor like any other (GitHub #622).

- Cases: `empty-required-values`, `relaxed-required-fields`
- Codes: `openapi/validation`
- Tests: `load.TestRequired_AnEmptyValueNoMinorAdmitsIsStillRefused`,
  `load.TestRequired_AnEmptyServerURLFollowsTheDeclaredVersion`,
  `load.TestRequired_ARelaxedFieldFollowsTheDeclaredVersion`

### `unmodeled-dialect-fields`

**A dialect's own field the parser does not model.** A source revision may add a field the bundled
parser's model names no member for; OpenAPI 3.2's `Response.summary`, XML `nodeType`, the nested
Encoding fields, a Media Type Object's `description` and `components/mediaTypes` are the live set.
Such a field is read off the raw node by a reader of its own, and the object census is *told* which
keys that reader took: the census grades a key by the parser's model, so left alone it reports a
field the document defines as an undefined key. The reader runs whatever version the document
declares, as [`later-version-construct`](#later-version-construct) requires, so the census never
calls such a field undefined.

The census is told what the reader actually took, never a key list by version. A value the parser
would decode as a string goes to its home — `summary` into `Docs.Summary`, `nodeType` into
`XMLHints.NodeType` — and is not also kept verbatim, because one declaration gets one structural
home (§12.1). A value of another shape, or a non-empty `nodeType` outside the five the specification
defines, is kept verbatim at the key and pointer the census itself uses, under `degraded_lowering`
and with a warning, and the census finds the entry and adds nothing. A key the raw mapping does not
present directly — merged in through `<<` — is not read at all, so the census's
`unknown-key-unreachable` still reports it. An empty or null value is read as unset. A reading with
no IR home is kept the same way under `ReasonNoIRHome`: the nested Encoding fields and the
`components/mediaTypes` section, into which this compiler follows no reference yet (GitHub #856). A
content entry written as such a `$ref` keeps it under `degraded_lowering` instead, since the entry
lowers without what it names. A Media Type Object's `description` is kept the same way under
`ReasonNoIRHome`, since `ir.Content` has no docs.

- Cases: `response-summary-32`, `xml-nodetype-32`, `nested-encoding-32`, `component-media-types-32`
- Codes: `openapi/degraded-construct`, `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryNoReaderTakesIsStillReported`,
  `annotation.TestRead_NodeTypeOnlyADefinedValueIsPlaced`,
  `operation.TestContent_MediaTypeDescriptionIsKept`

### `unreferenced-components`

a component entry of a section with no registry — responses, parameters, examples, requestBodies,
headers, links, callbacks, pathItems — that no lowering followed a reference to is kept verbatim on
the document under `openapi:components/<section>/<name>` (`no_ir_home`, no diagnostic) rather than
dropped, and 3.2's components/mediaTypes is kept whole, since no reference into it is followed
yet (GitHub #856)

**A component entry nothing references.** A compiler lowers a component where a reference
finds it, so an entry no reference reaches lowers nowhere — no node, no `Unmodeled` entry, no
diagnostic — and a declaration the document makes disappears in silence. Every component section
with no registry is therefore kept: each entry no lowering reached is preserved verbatim at
`Document.Unmodeled["openapi:components/<section>/<name>"]` under `ReasonNoIRHome`, at the entry's
own pointer, with no diagnostic. The sections are `responses`, `parameters`, `examples`,
`requestBodies`, `headers`, `links`, `callbacks` and `pathItems`; `components/schemas` and
`components/securitySchemes` are lowered unconditionally, and 3.2's `components/mediaTypes` is kept
whole until a reference into it is followed (GitHub #856). A section is read as the parser reads it,
through `<<` merge keys, so a merged-in entry is kept under its own name.

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

`ReasonNoIRHome` rather than a boundary, and no diagnostic, are the links precedent generalized: the
gap is one the IR can close by finding a use for the entry, so a promotion pass should read it, and
a document that declares a component is not making a mistake a warning could name. `Reason` is where
that decision lives (§12), which is what keeps a promotion pass from mistaking one for an oversight.

- Cases: `unreferenced-components`, `component-media-types-32`
- Codes: `openapi/degraded-construct`
- Tests: `openapi.TestUnreferencedComponents_ReferenceNothingFollowsKeepsItsTarget`,
  `openapi.TestUnreferencedComponents_ReadAsTheParserReadsThem`,
  `openapi.TestUnreferencedComponents_SelfReferenceByFileNameReaches`

## 2. Names and values

### `naming`

a name the document spells → `Naming{Source, Canonical}`: the spelling as written, beside the word
sequence §3.2 derives from it, at every position that spells one — `info.title`, a tag, an
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
  nor `Hint`, which #956 tracks for an enum member.

### `values`

a default, `const`, enum member or example → an `ir.Value` read off the YAML node by its resolved
tag alone, never by the type the schema declares (§6): `null`, `bool`, `string`, `list` and `object`
(members in source order, keyed by their text, an alias followed), a `!!binary` → `bytes`, decoded,
and a YAML timestamp → `string` in the spelling written (`2021-1-1` stays `2021-1-1`, never RFC
3339). A number is the literal's exact decimal text in `Num`, never a `float64`: a magnitude past
float64 or int64 keeps every digit, `.5`, `+5` and `09` take their JSON spellings (`0.5`, `5`, `9`),
`_` separators drop, a YAML integer base resolves (`0644` is `420`, `0x1f` is `31`, `0b1010` is
`10`), and digits, exponent and trailing zeros otherwise stand as written (`10.0`, `1.10e+3`). A
plain scalar that YAML resolves to a string only because it is past float64's range is still a
number when it is a finite decimal literal; a quoted one stays a string. A value no `Value` holds —
`.inf` or `.nan`, a `!!binary` that is not base64, a tag the compiler does not lower (`!acme`),
nesting past 128 — is not coerced: a default or example is dropped with an
`openapi/degraded-construct` warning at its position, and a `const` or an enum member is
[`enum-const`](#enum-const)'s. The number reader is the numeric bounds' as well
([`constraints`](#constraints)), which reports a bound it cannot read as
`openapi/invalid-numeric-literal`. A value kept verbatim under `Unmodeled` is a second channel that
keeps numbers and dates as written too, but a `!!binary` as its source text.

- Cases: `defaults`, `yaml-timestamp-scalars`, `numeric-precision`
- Codes: `openapi/degraded-construct`
- Tests: `value.TestNumericLiteral_YAMLBases`, `value.TestValueFromNode_Timestamp`,
  `annotation.TestSchemaExamplesAt_UnconvertibleValueIsReportedNotDropped`
- Status: partly built: a verbatim payload renders a number past float64 range as a string (#783),
  and a `<<` merge key in a mapping value stays a member named `<<` where a verbatim payload merges
  it (#395).

## 3. Schemas: identity, references and annotations

### `schema-registry`

components/schemas → registry (IDs from pointers)

- Cases: `named-types`, `component-reuse`
- Tests: `ids.TestForPointer_ChoosesTheNamespace`

### `inline-hoisting`

inline schemas hoisted with hints

- Cases: `inline-types`, `inline-hoist-positions`, `inline-annotations`

### `recursion`

a schema that reaches itself through `$ref` — directly, through a property, `items`,
`additionalProperties`, a union branch or an inline position, or through another component → a
`TypeRef` to the ID the schema was registered under before its body lowered, so the cycle lives in
the type graph by ID (§1), whichever declaration lowers first, with no diagnostic. A chain of
`$ref`s that never reaches a node without one (`A: {$ref: B}`, `B: {$ref: A}`) is not recursion but
a cycle with no schema in it: it is refused before anything lowers, as an `openapi/cyclic-ref`
error, and compiles to no Document.

- Cases: `recursive`
- Tests: `schema.TestLower_RecursiveSchemaTerminates`, `scan.TestDetectCycles_LegalRecursionClean`,
  `openapi.TestCompile_CyclicSpecDoesNotCrash`
- Status: partly built: an `allOf` that names its own schema, or two schemas that are each other's
  `Base`, lowers to a base chain that returns to where it started with no diagnostic, and a cycle of
  required properties that admits no finite value is reported by nothing (#642).

### `ref-use-site`

`$ref`-adjacent sibling keywords (3.1) and ref-target annotations merge onto the referencing
Property/Parameter with **use-site precedence**, applied uniformly (oagen's ad-hoc per-site patching
is the counterexample), and at a position carrying no Property/Parameter — an
`allOf`/`oneOf`/`anyOf` branch, `items`, a component — bind an alias hoisted at that position
instead, per §4.3 — constraints excepted, since bounds conjoin rather than override: each position
keeps the ones it declared and none is copied to a use site (§12.2)

- Cases: `param-ref-inheritance`, `allof-ref-branch-siblings`, `readonly-writeonly`
- Codes: `openapi/degraded-construct`
- Tests: `openapi.TestAnnotationRetention`, `schema.TestModel_RefSiblingDescriptionWins`,
  `schema.TestSchemaExamples_ComponentRefSiblingKeepsThem`

### `reference-object-siblings`

**A Reference Object's own siblings are the annotations of the use site.** OpenAPI lets the entry
that writes a `$ref` carry `summary` and `description` beside it, and the pair describes *that use*
of the referenced object rather than the object. Where the IR has a docs field at the position — a
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
ignored; there the fold is the 3.1 reading, and each sibling it reads is reported as a construct a
later minor added ([`later-version-construct`](#later-version-construct)).

- Cases: `ref-site-docs`, `later-version-constructs`
- Codes: `openapi/later-version-construct`

### `use-bound-annotations`

readOnly/writeOnly → Visibility and schema-level `default` → the referencing Property/Parameter's
Default: both bind a *use* of the type rather than the type, so a declaration-site one is pushed
down to referencing properties with use-site precedence and the declaration keeps its own copy
verbatim (`no_ir_home` Unmodeled + diagnostic) — which is what a component nothing references would
otherwise lose silently

- Cases: `defaults`, `readonly-writeonly`, `inline-residue`
- Codes: `openapi/degraded-construct`
- Tests: `openapi.TestAnnotationRetention_OtherDeclarationShapes`,
  `schema.TestPropertyPosition_ResidueStaysOutOfTheTypeNode`

### `docs-and-deprecation`

summary, description and externalDocs → `Docs`, and `deprecated: true` → `Deprecation{}`, on the
entity whose object writes them. `Deprecation` is an empty marker: the keyword states no message,
version or date, which only [`extension-promotion`](#extension-promotion) can supply. An operation,
and a tag on its `TagDef` and on the group it names, take all three; a schema's `title` is the
`Summary`, beside its `description` and `externalDocs`; a response and an error case take
`description` and `summary` ([`response-summary`](#response-summary)); a request body, a security
scheme, a server and a server variable take `description`, an example `summary` and `description`,
and a security scheme `deprecated` (3.2). `info` and the root `externalDocs` make the Document's one
`Docs`, while the Service takes `info.description` alone. A schema's documentation lands on the node
its position owns; where the position owns none (a shared primitive, a `$ref`), it lands on the
property, parameter or header that carries it, the referent's under the use site's field by field
([`ref-use-site`](#ref-use-site)). A parameter's or header's own `description` wins over its
schema's. `deprecated: true` on a parameter or header and on its schema each deprecate it, neither
one's `false` clearing the other. A request body's `description` lands on its Payload
([`request-body`](#request-body)), and a response's payload states no docs of its own.

- Cases: `docs-summary-desc`, `deprecation`
- Tests: `operation.TestOperation_DeprecatedIsCarried`,
  `operation.TestHeaders_DeprecationUnionsWithTheSchema`,
  `operation.TestContent_RequestBodyDocsReachThePayload`

### `disjoint-visibility`

a field both `readOnly` and `writeOnly` — on one schema, across a `$ref` whose site sets one flag
and referent the other, or on inline `allOf` branches that redeclare it — admits no lifecycle:
`Visibility{None: true}` (§5.2), lowered as declared, + `disjoint-visibility` warning, once, at the
invisible property or at the schema of the invisible header, or for `allOf` at the redeclaration
that empties the set (a branch repeating a field already invisible reports nothing more). It is not
a conflicting redeclaration ([`allof-redeclared-property`](#allof-redeclared-property)), which keeps
one declaration over another, nor a degraded construct: `None` states the intersection exactly, and
the warning says only that no request or response can carry the field. A flag set `false` states
nothing. A pair written where no `Visibility` can land — an `items` schema, a parameter's schema —
is kept verbatim and not reported ([`parameter-schema-keywords`](#parameter-schema-keywords)); a
component's is reported at each property that references it.

- Cases: `readonly-writeonly`
- Codes: `openapi/disjoint-visibility`
- Tests: `schema.TestModel_ReadOnlyAndWriteOnlyTogetherAreVisibleNowhere`,
  `openapi.TestAllOfVisibilityMerge_DisjointRestrictionsAreInvisibleNotAConflict`,
  `merge.TestReconcileProperty_AlreadyInvisibleVisibilityIsNotReAnnounced`

### `nullable`

`nullable`/type-arrays → Nullable

- Cases: `nullable-30`, `nullable-31-ref`, `nullable-enum-31`, `nullability-four-states`,
  `nullability-conjunction`
- Tests: `schema.TestSchemaRef_NullableNormalization`

### `schema-examples`

schema `example(s)` → Examples

- Cases: `examples`
- Codes: `openapi/degraded-construct`
- Tests: `annotation.TestSchemaExamplesAt_ReadsBothKeywordsInOrder`

### `xml-hints`

`xml` object (incl. 3.2 nodeType) → XMLHints at type and property level

- Cases: `xml-hints`, `xml-nodetype-32`
- Codes: `openapi/degraded-construct`
- Tests: `annotation.TestXMLHints_ReadsEveryFieldDirectly`, `schema.TestModel_PropertyXML`

## 4. Schemas: composition, unions and enumerations

### `allof-classification`

`allOf` → Base/Mixins per §4.3

How compilers classify `allOf`: an entry that is a `$ref` to a schema participating in a
discriminator hierarchy, or the sole `$ref` entry, becomes `Base`; other `$ref` entries become
`Mixins`; inline entries merge into `Properties` (with provenance). This mirrors Kiota's
decision table but keeps every branch reconstructible.

- Cases: `allof-inheritance`, `allof-mixins`, `allof-inline-merge`
- Tests: `schema.TestAllOf_DiscriminatorHierarchy`, `schema.TestAllOf_SoleRefBecomesBase`
- Status: partly built: a `$ref` to a subtype of a discriminated base, which declares no
  discriminator itself, is composed as a mixin rather than the `Base` (#986).

### `allof-beside-ref`

an `allOf` beside a `$ref` at the same level joins the alias's unhomed-keyword census the same way
(`degraded_lowering`), since a `$ref` site elects no composition family for it to lose or win

- Cases: `allof-ref-branch-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestRefSiteKeywords_KeptAtEveryPosition`

### `allof-inline-residue`

an inline `allOf` branch declaring more than the merge consumes → verbatim Unmodeled
(`degraded_lowering`) per §4.8

**An inline `allOf` branch declaring more than the merge consumes** — a branch written inline
rather than as a `$ref` owns no node, so §4.3's composition has nowhere to point and the branch
is merged into the composing model in place. That merge consumes the branch's `properties`, its
`required` list, and a `type` that is exactly `object` — which the composed `Model` already
restates — and nothing else. **The IR** could hold every other keyword a branch writes, and
does hold them one level up — the same keywords on the composing schema lower to `Docs`,
`Model.Additional`, `Model.Constraints` and `Unmodeled` — but attributing them to the composition
means either giving the branch a node (changing the composition shape) or merging them upward
with a precedence rule for branches that disagree. So a branch declaring anything the merge does
not consume is kept verbatim in `Unmodeled["openapi:allOf/<index>"]` under
`ReasonDegradedLowering` — the *whole* branch, consumed keywords included, so the source stays
reconstructable rather than having to be reassembled from two places — keyed by branch index so
siblings cannot overwrite each other, plus an `openapi/degraded-construct` diagnostic naming the
keywords that made it residue. Which keywords those are is derived from the branch's own declared
key set rather than from a fixed list, so a keyword the dialect gains later is residue without
anyone maintaining an enumeration. The key set is read through YAML aliases and merge keys, so a
branch written `[*shared]` or `[{<<: *shared}]` derives the same residue as one written out. The
diagnostic is a `warning` rather than an `info` where the branch declares a `type` that excludes
`object`, because the composing `Model` then asserts a shape the source contradicts — `allOf:
[{type: string, maxLength: 3}]` composes an empty model of kind `model` over a source that says
`string`. Two branch kinds are outside this rule rather than limits of it: a `$ref` branch owns a
node, so what is written beside the `$ref` is *modeled* on the branch alias §4.3 describes rather
than kept as residue, and a boolean branch declares no keywords at all, so there is no residue to
derive — it is a decision about the composed node instead, stated with the `false`-schema rule
[`allof-false-branch`](#allof-false-branch).

- Cases: `allof-inline-residue`
- Codes: `openapi/degraded-construct`, `openapi/unpreservable-construct`
- Tests: `schema.TestAllOf_BranchWrittenByReferenceStillDerivesResidue`,
  `schema.TestAllOf_MergedBranchKeywordsAreNotResidue`,
  `schema.TestAllOf_UnpreservableBranchIsNotAnnounced`

### `allof-redeclared-property`

a property redeclared across branches with a type, constraint keyword, default, description,
examples, deprecation or `xml` the merge drops → the losing declaration's node verbatim Unmodeled
(`degraded_lowering`) under `openapi:conflicting-redeclaration<pointer>` per §4.8, keyed by the
losing declaration's pointer, with the `openapi/conflicting-redeclaration` diagnostic only where the
two are unsatisfiable, an info `degraded-construct` naming a detail held differently, and
nullability intersecting rather than dropping where the targets agree

**A redeclared property the merge cannot fold whole** — `allOf` intersection folds a field
declared in more than one branch into one `Property`, and the first declaration wins its shape
(§4.3). The IR has no combinator for "string here, integer there", nor a second slot for a
default, description, examples, deprecation or XML hint the first declaration already fills, so
whatever of the redeclaration the fold cannot carry has nowhere to sit on the merged property —
and before this rule it went nowhere at all: a release in which a losing branch's type or
default changed produced no change in the document, which a consumer diffing two versions reads
as nothing having happened. The losing declaration is kept verbatim, as the document wrote it,
in `Unmodeled["openapi:conflicting-redeclaration<pointer>"]` under `ReasonDegradedLowering`,
keyed by its own JSON Pointer so a field three branches type three incompatible ways keeps all
three entries, and with the entry's `Provenance` locating that declaration rather than the
merged property. The source node is kept rather than the IR values the merge dropped: §12
defines an `Unmodeled` value as the source construct, a `TypeID` is a compiler-minted registry
ID that `irverify`'s reference walk cannot see dangle inside a byte slice, and one node keeps
everything the redeclaration said at once — a `$ref` as the `$ref` written, a nullability, a
default. Preservation is owed wherever something is dropped, which is wider than the
disagreements worth a warning: the `openapi/conflicting-redeclaration` diagnostic fires only
where the two declarations are genuinely unsatisfiable (an incompatible type, or one constraint
keyword pinned to two values), a detail the first declaration already holds differently is
named in an info `openapi/degraded-construct` diagnostic, and two composites of one kind, an
unresolvable target, and the top type against anything are dropped without any diagnostic and
kept all the same. The loser's `default`, constraints and examples go with its type rather than
folding onto the winner — they describe the shape that lost, and adopting them would leave one
field asserting two contradictory things, with nothing downstream comparing a value's kind to
its property's type; this is keyed on the type being dropped, not on the drop being called a
conflict. Two declarations naming the same target and differing only in nullability are not a
drop and take no entry: the merged field admits null only where both branches do, which is the
same conjunction a single schema's null verdicts fold under.

- Cases: `allof-conflicting-type`
- Codes: `openapi/conflicting-redeclaration`, `openapi/degraded-construct`,
  `openapi/unpreservable-construct`
- Tests: `merge.TestReconcileProperty_NullabilityIntersectsWhenTargetsAgree`,
  `merge.TestReconcileProperty_ADroppedTypeIsKeptEvenWhenItDoesNotConflict`,
  `merge.TestReconcileProperty_ALosingDetailIsKeptAndReported`
- Status: partly built: a primitive a `format` narrows is reported as conflicting with the bare
  primitive, which it does not contradict (#446).

### `allof-required`

a `required` name written on an `allOf` branch, inline or beside a branch's `$ref`, or beside the
`allOf` itself → `Property.Required` (§5.1) on the composed `Model`'s own property of that wire
name, whichever branch declares the property: a branch holding nothing but `required` attaches
across the whole composition, not only its own `properties`, and an attachment never clears a
`Required` already set. A branch is read from its local schema, never from its `$ref` target, whose
`required` list belongs to the target's own model. A name that matches no property the composed
`Model` declares — one only a `Base` or `Mixin` declares, or one nothing declares — has no field to
land on, because a `Model` stores its own properties and no flattened view across `Base`/`Mixins`
(§4.3): it is reported as `openapi/unattachable-required` at the branch or schema that wrote it, a
warning where the `Model` composes a `Base` or `Mixin` that may declare it, an info where it
composes none.

- Cases: `allof-required-only`
- Codes: `openapi/unattachable-required`
- Tests: `schema.TestAllOf_CompositionRequiredAttaches`,
  `schema.TestAllOf_RequiredOnlyBranchNamingBaseOwnedPropertyDiagnosed`,
  `schema.TestAllOf_RequiredOnlyBranchNoBaseOrMixinDiagnosedInfo`
- Status: partly built: the unattached name is reported and kept nowhere, where a map keeps its
  `required` verbatim (#896).

### `allof-false-branch`

a boolean `false` `allOf` branch → the composed Model closed, branch verbatim Unmodeled
(`degraded_lowering`) per §4.8, a `true` branch a silent no-op

The `false` rule ([`whole-schema-fallbacks`](#whole-schema-fallbacks)) holds wherever the schema
appears, composition included. A `false` conjunct (`allOf: [false, …]`) admits nothing, so the
composition admits nothing: the composed `Model` is closed and the branch is kept verbatim under
`ReasonDegradedLowering` beside it, which is what distinguishes it from a model that merely wrote
`additionalProperties: false`. What the other branches contributed stays — closing is the nearest
shape the IR has, and emptying the model would trade one silent loss for another. A `true` conjunct
admits everything, so contributing nothing from it is exact and nothing is reported.

- Cases: `allof-boolean-branch`
- Codes: `openapi/false-schema`

### `union`

`oneOf`/`anyOf` → Union (Exclusive bit)

- Cases: `oneof-discriminated`, `anyof-untagged`

### `union-null-variant`

null-variant → Nullable ref (a keyword beside the collapsed union that no reference holds — a
`discriminator`, a `format` other than `password`, a collection bound — kept verbatim Unmodeled,
`degraded_lowering`, on the node or carrier owning the position, as at a `$ref` site)

- Cases: `nullable-union-discriminator`, `nullable-31-ref`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestCollapsedUnion_KeepsEveryCensusKeywordItCannotHold`,
  `schema.TestCollapsedUnion_KeepsItsDiscriminatorAtEveryPosition`

### `union-null-only`

a null-only branch set (every branch a bare `type: null`) → the same nullable `any` a bare
`{type: null}` schema at that position lowers to, with the oneOf/anyOf itself kept verbatim
Unmodeled (`degraded_lowering`) since no node built for it carries a branch set

- Cases: `null-only-union`
- Codes: `openapi/degraded-construct`

### `union-beside-structure`

co-declared with structural keywords → the composition distributed across the variants per §4.3, or
— for the five shapes that cannot be distributed — structural body + verbatim union per §4.8
(branches that declare no shape at all are `validation_only` per §4.7), with a declared
`discriminator` beside it staying on the model only when every type it routes to is a subtype of it,
decided once every node is interned, and otherwise kept verbatim in
`Unmodeled["openapi:discriminator"]` (§4.8), a body the move leaves a map and nothing else becoming
its `MapT` (§4.6)

**JSON Schema `oneOf`/`anyOf` co-declared with structural keywords that cannot be
distributed** (`{type: object, properties: {…}, oneOf: [{$ref: A}, {type: string}]}`) — the
keywords conjoin, so the schema is an *intersection* of a structural type and a union.
**The IR** has `Union` but no intersection combinator (§4.3's `Base`/`Mixins` composes one
referent per conjunct, the shape `allOf` writes, and cannot state "conjoined with a union";
§15 records why one was not added) — target languages are not the constraint here; TypeScript
writes `{a: string} & (X | Y)` directly. Where the conjunction *distributes* it is not degraded
at all: §4.3 gives the rule, and the result is an ordinary `Union` of composed `Model`s. What
lands here is the residue — the five shapes distribution cannot reach:
1. the structural body is not a `Model` (a `const`, an `enum`, a scalar, or a bare
   `required`/`items`/`prefixItems`/`additionalProperties`/`patternProperties` with neither
   `properties` nor a `type`), so there are no composition fields for the variants to carry;
2. a branch is written inline rather than as a `$ref`, so it names no node for `Base`/`Mixins`
   to point at — a `type: null` branch included;
3. a branch *is* a `$ref` but names no referent the compilation resolves — a cross-document
   target it does not intern, an undeclared component, a pointer addressing no schema. The
   branch is unusable for the same reason an inline one is, so it declines the same way rather
   than composing a variant whose branch quietly went missing; the broken reference is reported
   at `error` severity on top of the `info` below;
4. a `discriminator` is declared, binding the branch schemas by name;
5. `oneOf` and `anyOf` are both declared, so distributing one would drop the other.

A `discriminator` beside any of the five whose body lowers to a `Model` stays on that `Model`
only when every type it routes to is a subtype of it at any depth (`ir.IsSubtype`, the rule
`pass/validate` holds a model discriminator to): each `mapping` target, the `defaultMapping`,
and each component a `$ref` branch names, which it routes to by name — `oneOf`'s branches where
`oneOf` and `anyOf` are both declared, since `oneOf` wins. An inline branch, or one naming no
component, is no route unless the `mapping` names it. Otherwise the whole object
(`propertyName`, `mapping`, `defaultMapping`) is kept verbatim in
`Unmodeled["openapi:discriminator"]` on the node keeping the union, with an `info` diagnostic at
the discriminator; a subtype keeps the `DiscriminatorValue` its mapping key gives it, since that
is still the tag its instances carry. A union whose branches declare no shape
(`validation_only`) is held to the same rule, and so is a multi-typed body, which lowers to a
`Union` whose object variant, built from the same schema, holds the discriminator. The compiler
decides once every node is interned (`compile.Settlement`), because neither the walk nor the
source schemas can answer it: a subtype declared after the `Model` has no `Base` while the
`Model` lowers, and a schema whose `allOf` names the `Model` may still lower to no subtype — an
`enum`, a `const`, an alias over a `$ref` beside it, or a union its `allOf` is distributed
across.

In all five, whatever structural body the schema declares is lowered — `properties` or a
declared `type` yields a Model or typed node — and the union branches are kept verbatim in
`Unmodeled["openapi:oneOf"]` / `["openapi:anyOf"]` + `info` diagnostic naming which of the five
applied. Case 1's body-less sibling leaves nothing to build: it reduces to `any`, and the
sibling itself is kept by [`shape-applicator`](#shape-applicator) rather than dropped, so the
union's diagnostic and the sibling's are both reported. A `null` branch in this shape is **not**
lifted to `Nullable`: the union it belongs to is no longer structural, so lifting one branch out of
it would assert something the lowered type does not say. The degradation is that the generated SDK
is *under-constrained* — it admits values the union would reject — which is why the branches must
survive for a validating emitter to reach.

Where a union's lowering leaves no `Model` or `Union` to route by it — a union beside a
`const`, `enum` or single-typed scalar body, a null-only branch set, a `$ref` site (alias or
carrier), or a `{X, null}` union collapsed to a nullable reference (§3.3) — a `discriminator`
beside it is kept as any keyword its node cannot carry is: the whole object goes to
`Unmodeled["openapi:discriminator"]` on the node or carrier that owns the position, with an
`info` diagnostic. The collapse is held to a `$ref` site's rule (§4.3), since it too lowers to a
reference, so a `format` or collection bound beside it is kept the same way. A multi-typed body
with no `object` among its types runs no census, so it still drops one (#852).

A union whose branches declare no shape at all (`oneOf: [{required: [a]}, {required: [b]}]`)
is not a degradation and is not listed here: it narrows the body without reshaping it, which
is validation logic, so it is preserved under `ReasonValidationOnly` with §4.7's keyword family.

- Cases: `allof-oneof-cooccurrence`, `discriminated-union-beside-model`, `unhomed-keywords`
- Codes: `openapi/composition-lowering`, `openapi/degraded-construct`, `openapi/unresolved-ref`,
  `openapi/validation-only-keyword`, `openapi/unpreservable-construct`
- Tests: `schema.TestOneOf_CoDeclaredNotDistributedReasons`,
  `schema.TestOneOf_CoDeclaredUnresolvableBranchIsNotDistributed`,
  `schema.TestDiscriminatorBesideUnion_LeavesAMapWhenItMoves`

### `union-beside-ref`

the same co-declared `oneOf`/`anyOf` beside a `$ref` at the same level, rather than a structural
body, has nothing at that position to distribute the union across either, so it lowers to the alias
over the `$ref` target with the union kept verbatim beside it (`degraded_lowering`), through the
same keeper as the structural-body case

- Cases: `union-beside-ref`
- Codes: `openapi/degraded-construct`

### `competing-keywords`

competing keywords at one position — `const`/`enum`/`allOf`, elected in that order; `oneOf` beside
`anyOf`, where oneOf wins; and a parameter's or header's `schema` beside its `content`, where
content wins, since a media-type entry names both a schema and the media type serializing it and the
IR models both — lower as the elected keyword with every passed-over one verbatim Unmodeled
(`degraded_lowering`) at its own pointer per §4.8, and a `{X, null}` oneOf beside an anyOf stays a
Union rather than collapsing to a nullable ref

**Competing keywords at one position, where only one can be the value** — keywords conjoin, so
`{allOf: [{$ref: Base}], enum: [a, b]}` is a narrowing of `Base` rather than a malformed document
and `{oneOf: […], anyOf: […]}` demands both, yet a position lowers to one node. Two families
compete: the value keywords `const`, `enum` and `allOf`, elected in that order, and the two union
combinators, where `oneOf` wins. **The IR** has no intersection combinator (§15, as for
[`union-beside-structure`](#union-beside-structure)), so the position lowers as the elected keyword
— the most it can express there — and each keyword the election passed over is kept verbatim in
`Unmodeled["openapi:<keyword>"]` under `ReasonDegradedLowering` at that keyword's own pointer, plus
one `openapi/degraded-construct` `info` naming what was elected and what was kept — a `warning`
where a parameter's or header's `schema` is passed over for its `content`. A position reports only
what it actually stored, so a payload that fails to convert is reported unpreservable rather than
announced as kept. Two consequences are deliberate: the elected combinator still becomes a `Union`
rather than degrading to the top type, because that `Union` is a shape the IR *can* express and
discarding it as well would model nothing at all; and a `{X, null}` `oneOf` written beside an
`anyOf` does **not** collapse to nullable `X` (§3.3), because the collapse asserts the position *is*
nullable `X` while the co-declared `anyOf` conjoins with it, and collapsing would resolve the
position to a shared primitive — which must never carry one declaration's keywords. Where a
*structural* sibling is written as well, [`union-beside-structure`](#union-beside-structure) applies
instead and both branch sets are kept. A keyword the *elected* lowering never reads — `type: string`
beside an `allOf`, `format` beside a `const` — is kept by the census
[`shape-applicator`](#shape-applicator) runs, which asks the node that was built: `type: object`
beside an `allOf` is one the composed `Model` already asserts, and keeps nothing (GitHub #268).

- Cases: `codeclared-keywords`, `codeclared-schema-content`
- Codes: `openapi/degraded-construct`, `openapi/unpreservable-construct`
- Tests: `schema.TestCoDeclaredFamily_EveryPassedOverKeywordIsKept`,
  `schema.TestCoDeclaredFamily_UnpreservableIsNotAnnounced`,
  `schema.TestUnionCombinators_UnpreservableIsNotAnnounced`

### `discriminator`

`discriminator` → Discriminator (3.2 `defaultMapping` → Discriminator.Default), beside a node that
does not route by it (neither a Model nor a Union) → verbatim Unmodeled (`degraded_lowering`) per
§4.8 (a multi-typed schema excepted, which runs no census yet, #852), and a subtype the mapping
names under several keys — an alias tag — takes the smallest in byte order as its
DiscriminatorValue, since the field holds one and a mapping's key order is not a property of the
document, with every key kept in the base's Mapping and the election reported (`degraded-construct`,
info)

- Cases: `oneof-discriminated`, `discriminator-inheritance`, `discriminator-default-mapping`,
  `discriminator-alias-mapping`, `nullable-union-discriminator`
- Codes: `openapi/degraded-construct`, `openapi/unresolved-ref`
- Tests: `schema.TestHomelessDiscriminator_IsKeptBesideEveryOtherNode`,
  `schema.TestUnplacedDiscriminator_IsKeptAtEveryUnionSite`

### `discriminator-value`

a subtype's wire tag → `Model.DiscriminatorValue`, left empty on the base itself. It is read off the
discriminators of the subtype's `allOf` ancestors — the `$ref` branches' targets, then theirs, level
by level to 256 levels, nearest first, a schema that declares none passed through and no schema its
own ancestor — as the key an ancestor's `mapping` spells for the subtype, the mapping naming it by
reference or by bare schema name, an inline subtype by its position's reference, and where several
keys name it [`discriminator`](#discriminator) elects one. A key of `""` is a tag, not an absence.
Once no ancestor's mapping names it, the tag is the subtype's schema name as declared (`Cat/Dog`,
not its pointer token) if it is a component schema, OpenAPI's implicit mapping. A subtype that is no
component schema — inline as a property or a union branch, or under an extension — has no name to
answer to, so only a mapping entry tags it, and a chain with no discriminated ancestor carries no
tag however deep. A variant of a distributed `oneOf`/`anyOf`
([`union-beside-structure`](#union-beside-structure)) carries the tag of the schema enclosing it.

- Cases: `discriminator-transitive`, `discriminator-inline-subtype`
- Tests: `schema.TestAllOf_DiscriminatorSubtypeValue`,
  `schema.TestAllOf_DiscriminatorValueFromNearestAncestor`,
  `schema.TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue`

### `string-const-union`

a oneOf/anyOf whose variants are all string consts normalizes to a closed `Enum` in a `pass/`
normalization — not in the compiler — so per-variant `Docs` survive until the collapse is chosen

- Status: partly built: the compiler keeps such a union a Union of Literals with per-variant docs,
  and no pass normalizes it to an Enum yet (#990).

### `enum-const`

`enum` → Enum at any member count, or a Union of Literals where an Enum cannot hold its members, and
`const` → Literal (§4.5); `enum: []` → a closed Enum with no members + `empty-enum` warning, a
member or `const` value that cannot convert → `any` + `degraded-construct` warning, and an `enum`
past the member budget → `any` + `budget-exceeded` error

- Cases: `enum-string`, `enum-numeric`, `empty-enum`, `enum-member-identity`, `nullable-enum-31`,
  `literal-const`
- Codes: `openapi/empty-enum`, `openapi/duplicate-enum-value`, `openapi/degraded-construct`,
  `openapi/budget-exceeded`
- Tests: `schema.TestEnum_HeterogeneousBecomesUnionWithBadValue`,
  `schema.TestEnum_NonScalarAndMidListMismatch`,
  `schema.TestEnumAsUnion_UnconvertibleMemberBecomesAny`

## 5. Schemas: objects, arrays and scalars

### `additional-properties`

`additionalProperties: false` → Additional=closed, `additionalProperties: true` or a schema →
AdditionalProps (`true` typed as the top type, as `{}` is), and an object whose one member fact that
catch-all is → a `MapT` per §4.6, `unevaluatedProperties: false` → closed_after_composition,
`minProperties`/`maxProperties` → Model.Constraints (the property set's cardinality, as against
Additional's openness) or MapT.Constraints (its entry count)

**`true` writes a catch-all; an absent keyword does not.** In JSON Schema,
`additionalProperties: true` is the empty schema, so it types the catch-all as the top type exactly
as `{}` does: alone it is a `MapT` of the top type, and beside properties an `AdditionalProps` of
the top type. An absent `additionalProperties` states nothing, so `{type: object}` stays a `Model`
with no properties and `Additional` unspecified; whether such an object is a free-form map is a
target's policy, not a fact the source declared. The one catch-all the source does not write is
beside `patternProperties`: `AdditionalProps.Value` is not optional, and a key no pattern matches is
unconstrained, so it is the top type, and beside patterns the IR does not tell an absent
`additionalProperties` from `true`. That object stays a `Model`, since a `MapT` has no field for a
pattern-scoped value. An `unevaluatedProperties: false` beside a catch-all is inert in JSON Schema
but still lowers to a closure, which keeps the object a `Model` until #862 is resolved.

- Cases: `maps`, `constraints`, `allof-position-constraints`
- Tests: `schema.TestObject_CatchAllBesideAnotherFactStaysModel`,
  `schema.TestObject_MapAndNothingElseLowersToMapT`
- Status: partly built: `additionalProperties: false` beside `patternProperties` also carries a
  catch-all typed as the top type, which the closure contradicts (#987).

### `pattern-properties`

`patternProperties` → AdditionalProps.Patterns

- Cases: `maps`
- Tests: `schema.TestModel_PatternPropertiesOrder`

### `missing-array-items`

a `type: array` schema with no `items` in a 3.0 document, where 3.0 requires one → lowered as
2020-12 reads it, a `List` over the top type, + `missing-array-items` warning once at the schema's
own pointer, wherever it sits (a property, a parameter's schema, an `items`, a union branch) and
whatever it lowers to (a `Tuple`, an enum of arrays), since the report reads the declaration and not
the node. Nothing is dropped or degraded, so no `degraded-construct` accompanies it. `items: {}` is
silent, as is every schema at 3.1 and 3.2, where `items` is optional. Nor is a schema reported
beside a `$ref`, whose siblings 3.0 ignores, an inline `allOf` branch, which merges in place, or a
subschema kept verbatim, such as one under `not`.

- Cases: `missing-array-items`
- Codes: `openapi/missing-array-items`
- Tests: `schema.TestLower_MissingArrayItems`, `schema.TestLower_ListWithoutItems`,
  `schema.TestLower_MissingArrayItemsLoweredTwiceWarnsOnce`

### `tuples`

`prefixItems` → Tuple.Elems and the `items` beside it → Tuple.Rest (`items: false` → none, an absent
`items` → the top type, as 2020-12 reads it `true`), with `minItems`/`maxItems`/`uniqueItems` →
Tuple.Constraints

The OpenAPI compiler reads `Rest` from the `items` beside `prefixItems`, which 2020-12 applies to
every element past them: `items: false` leaves no `Rest`, an absent `items` reads as `true`, and
either of those or a schema lowers exactly as a `List`'s `items` would, at the same pointer and
under the same hint. An `unevaluatedItems` beside them stays the §4.7 keeper's verbatim entry, as it
does beside a `List`. As there, `items: true` and an absent `items` lower alike, though only the
first makes that `unevaluatedItems` inert; recording the difference is #862.

- Cases: `tuples-prefixitems`
- Codes: `openapi/validation-only-keyword`
- Tests: `schema.TestLower_TupleRest`

### `shape-applicator`

a shape applicator
(`properties`/`patternProperties`/`additionalProperties`/`required`/`items`/`prefixItems`, and a
`format` other than `password` where no type is declared) the lowered node has no field for →
verbatim Unmodeled (`degraded_lowering`) per §4.8

**A shape applicator the lowered node cannot carry** — `properties`, `patternProperties`,
`additionalProperties`, `required`, `items` and `prefixItems` each constrain an instance whether or
not a `type` is written beside them, and each has exactly one IR home: a `Model`'s property set,
openness and pattern bindings, or a `List`'s element and a `Tuple`'s positions and rest. A `MapT`
homes an object's catch-all and the empty `properties`, `patternProperties` and `required` lists
that left it a map (§4.6), but not a `required` that names a key. A position whose lowering produced
none of these — an `Enum`, a `Literal`, a `Scalar`, or the top type — has nowhere to put them, and
so does a position whose lowering produced the other of the two shapes
(`{type: object, properties: {…}, items: {…}}` lowers to a `Model`, which holds no element type).
This covers both a contradictory schema, where one half is taken and the other must stay recoverable
(`{type: string, enum: [a, b], properties: {f: …}}` is not satisfiable, so lowering to the enum is a
reasonable degradation), and an applicator written with no type at all, where the position lowers to
the top type — which says strictly less than the applicator does (`{items: {type: string}}`
constrains every array instance). **The IR** could model the untyped case by reading the applicator
as the shape it implies — an untyped `items` as a list — but that is a lowering decision this
document has not taken, and keeping the source leaves it open. Each unhomed keyword is kept verbatim
in `Unmodeled["openapi:<keyword>"]` under `ReasonDegradedLowering`, plus one
`openapi/degraded-construct` `info` naming the kind the position lowered to and the keywords it
cannot hold. Which keyword has a home is asked of the node the lowering produced, never re-derived
from the dispatch that produced it. A position that reduced to a shared node gets an alias of its
own first: a shared primitive must never carry one declaration's keywords. The `format` keyword
joins the list wherever the lowered node does not pair it with a primitive — no `type` declared, or
an object, an array or an enum — since it is homed by pairing with a type rather than by a node
kind. The census reads `type`, `const`, `enum`, `minItems`, `maxItems` and `uniqueItems` beside
these, so a collection bound on an object is kept the same way (GitHub #268). `discriminator` joins
the list too, homed by a `Model` or `Union` that routes by it and by nothing else. One limit is
deliberate: a multi-typed schema (`type: [string, object]`) is exempt, because it lowers one variant
per declared type from the same schema and the homes are the variants' — though a keyword no variant
holds is then dropped (#852). At a `$ref` position the census runs on the alias §4.3's use-site rule
hoists, since the siblings bind the position rather than the referent.

- Cases: `unhomed-keywords`, `empty-enum`
- Codes: `openapi/degraded-construct`, `openapi/unpreservable-construct`
- Tests: `schema.TestObject_MapKeepsARequiredListVerbatim`,
  `schema.TestUnhomedApplicator_KeptOnAListAndAnnounced`,
  `schema.TestUnhomedApplicator_FormatWithNoTypeIsKept`

### `string-formats`

a `type: string` with a `format` the table names → the shared primitive the pairing selects, and no
node of its own: `date` → `date`, `time` → `time`, `duration` → `duration`, `uuid` → `uuid`, `uri` →
`url`, `date-time` → `datetime_offset`, `binary` → `bytes`. `byte` → a hoisted `Scalar` over `bytes`
whose `Encoding` is `base64`, with the `string` it travels as in `WireType`. Any other format
(`email`, `uri-reference`, `ipv4`, an invented one) → a hoisted `Scalar` over `string` whose
`Encoding.Name` is the format as written, case kept, since the compiler knows no registry of formats
beyond the table. A `contentEncoding` beside a `byte` or unknown format names the encoding instead;
where the two name different encodings the format is kept verbatim under `openapi:format`
(`no_ir_home`) with an info `degraded-construct`. A scalar hoisted for its format carries the bounds
written beside it. The table is the same at every minor, and a pairing of `format` with another
`type` is [`numeric-formats`](#numeric-formats)'s.

- Cases: `encoding-byte`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_UnknownFormatPerBaseType`,
  `schema.TestLower_NamedScalarComponentResolves`,
  `schema.TestFormatTable_EveryRowDistinguishesTheBareType`

### `numeric-formats`

a numeric `type`+`format` → the primitive the format names — an integer width under `integer` and
`number` alike, since OpenAPI applies a format to both (3.1.1 §4.4.1), and
`float`/`double`/`decimal`/`decimal128` under `number` alone, since on an integer they admit only
integral values, which no primitive narrower than `integer` holds — and a pairing no primitive names
→ a `Scalar` over the type's primitive that keeps the format, as `Encoding.Name` or, where a
`contentEncoding` beside it takes that field, verbatim Unmodeled

- Cases: `scalar-format`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestScalar_NumericFormatPairings`,
  `schema.TestContentVocabulary_KeptWhereNoEncodingHolds`

### `password`

`format: password` → a redaction request rather than a (type, format) pairing, so the shape lowers
without it and it lands where a declaration's annotations do: `Sensitive` on the node the position
owns, whatever its kind (an alias hoisted where the shape alone would own none), or, at a property
or header that owns none, `Property.Secret`, inherited from a `$ref` target as `deprecated` is; at a
parameter that owns none it is kept verbatim in Parameter.Unmodeled (`no_ir_home`), ir.Parameter
having no redaction field (GitHub #578)

- Cases: `secret-format`, `scalar-format`
- Codes: `openapi/degraded-construct`

### `whole-schema-fallbacks`

Two whole-schema fallbacks sit outside §4.8's list, because they answer to a compiler's own limits
rather than to anything about the construct: a schema nested past the compiler's lowering depth cap
becomes `any` with an `error` diagnostic, and a boolean `false` schema becomes a closed empty
`Model` with an `info` one and the schema itself kept verbatim under `ReasonDegradedLowering` at
`Unmodeled["openapi:schema"]` — the key names the position because a boolean schema writes no
keyword to name it by, and nothing can collide with it, since such a schema has nothing else to
preserve.

That preservation is load-bearing, not decorative: a closed empty `Model` admits `{}`, and the
source admits nothing, so the lowered type is one value wider than what was written. Without the
verbatim entry, `false` and `{type: object, additionalProperties: false}` are the same node, and
nothing downstream can tell "no instance" from "the empty object". The `info` diagnostic says which
one it was, but a diagnostic is not part of the document.

The approximation is preferred to an exactly empty shape, and the reason is worth stating because
the IR does have one. A closed `Enum` with no members is an exactly empty value space (§4.5), but
`Enum.ValueType` is a `PrimKind` and a `false` schema declares no type, so lowering to one means
inventing a value type the source never wrote — an exact-looking node resting on a fabricated
field, which is not more faithful than an approximate one with the source beside it. It would also
split the rule: the composition case ([`allof-false-branch`](#allof-false-branch)) must stay a
`Model`, because it keeps what the other branches contributed, so one source construct would lower
to two different kinds depending on where it appeared. The overstatement is bounded, recorded, and
recoverable from the `Unmodeled` entry; an emitter that wants the exact reading has everything it
needs to produce it.

- Cases: `allof-boolean-branch`
- Codes: `openapi/degraded-construct`, `openapi/false-schema`
- Tests: `schema.TestLower_DepthCapExceeded`,
  `openapi.TestMaxNesting_AdmitsWhatTheLoweringLowersInFull`

## 6. Schemas: keywords beyond the shape

### `constraints`

`minimum`, `maximum` and `multipleOf` → `Min`, `Max` and `MultipleOf`, and `minLength`, `maxLength`
and `pattern` → `MinLength`, `MaxLength` and `Pattern` as written, on the node or carrier the
position owns (a `Scalar`, a `Property` or a `Parameter`); `minItems`, `maxItems` and `uniqueItems`
→ the `List`'s, and a `Tuple`'s per [`tuples`](#tuples); `minProperties` and `maxProperties` per
[`additional-properties`](#additional-properties). A bound is read off the raw node, never the
parser's `float64`, so its `BigVal` keeps every digit the literal wrote ([`values`](#values)), a
quoted one as the decimal it spells; one that is no finite decimal literal is dropped with an
`openapi/invalid-numeric-literal` error at the schema that wrote it. Exclusive bounds follow the
declared dialect. In 3.0 the boolean modifies the bound beside it: `true` moves that bound to
`ExclusiveMin` or `ExclusiveMax` and clears `Min` or `Max`, so a 3.0 document and its 3.1
translation lower alike, `false` states nothing, and `true` with no bound beside it bounds nothing
and is kept verbatim under `openapi:exclusiveMinimum` or `openapi:exclusiveMaximum`
(`degraded_lowering`) with a `degraded-construct` warning. In 3.1 and 3.2 the keyword is a bound of
its own, kept beside `Min`/`Max` when both are declared, a field apiece and no `Unmodeled` entry
(§5.3). The other dialect's form (a boolean in 3.1 or 3.2, a number in 3.0) is dropped with an
`openapi/invalid-exclusive-bound` error. A bound beside a node with no `Constraints` field to take
it (an `Enum`, a `Literal`, a `List` for a value bound) is kept verbatim under `openapi:<keyword>`
(`degraded_lowering`) with an info `degraded-construct` naming the node it lowered to, as
[`shape-applicator`](#shape-applicator) keeps a collection bound on an object.

- Cases: `constraints`, `numeric-precision`
- Codes: `openapi/invalid-numeric-literal`, `openapi/invalid-exclusive-bound`,
  `openapi/degraded-construct`
- Tests: `openapi.TestConstraints_ExclusiveBoolean30`,
  `openapi.TestConstraints_ExclusiveWrongDialectForm`,
  `openapi.TestConstraints_LosslessNumericLiterals`
- Status: partly built: a bound beside a `oneOf` or `anyOf` that lowers to a `Union` is dropped in
  silence (#848).

### `validation-only`

`not`/`if-then-else`/`dependentSchemas`/`dependentRequired`/`contains`/`propertyNames`/`unevaluated*`
→ verbatim Unmodeled per §4.7

- Cases: `negation-not`, `dependent-required`
- Codes: `openapi/validation-only-keyword`, `openapi/unpreservable-construct`
- Tests: `annotation.TestValidationOnlyAt_KeepsEveryKeywordItClaims`,
  `schema.TestLower_ValidationOnlyKeywords`,
  `annotation.TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept`

### `dynamic-ref`

`$dynamicRef` → the anchored type by compiler expansion, else verbatim Unmodeled with the reason it
was irreducible

`$dynamicRef` is resolved per reference site by compiler expansion. What makes that possible is
that a document's set of `$dynamicAnchor` declarations is fixed before evaluation: a name declared
exactly once is the only match any dynamic scope containing a match can hold, so it *is* 2020-12
§8.2.3.2's outermost matching anchor whatever path evaluation took. That argument assumes the
document declares no `$id`, which the compiler enforces rather than hopes for: `$id` starts a new
schema resource, so an `$id` anywhere on the path between a reference and a candidate anchor makes
the reference irreducible.

A reference is irreducible — and is then kept verbatim under `ReasonDegradedLowering` with an
`info` diagnostic naming which case it was — when the name is declared more than once (the target
is back under the evaluation path's control), when it is unresolvable, names another document, or
is not a plain fragment, when its value is not a string at all, when a resource boundary stands
between it and the anchor as above, when expanding it would close a cycle of aliases (a `$ref`
cycle is refused outright for the same reason, and expansion must not mint one the refusal cannot
see), or when it is co-declared with a `$ref`, a `oneOf`/`anyOf`, a shape of its own, or one of
`items`/`prefixItems`/`required` — an expansion takes its target whole, so it has nowhere to put a
sibling applicator. One further case is irreducible in the OpenAPI compiler specifically: an anchor
declared anywhere but the top level of `components/schemas` has no ID that is stable without
consulting the registry, and resolving it would make the reference depend on which position
happened to lower first (§4.3), so it is preserved instead.

Anchors are counted through YAML aliases and merge keys, so an anchor reached only by an alias
still makes its name a duplicate. The walk is bounded by depth and by node count, and says so with
a `warning` when it truncates, since a truncated index undercounts declarations and an undercount
is what turns "irreducible" into a confident expansion.

- Cases: `dynamic-ref`
- Codes: `openapi/dynamic-ref-expanded`, `openapi/degraded-construct`
- Tests: `schema.TestDynamicRef_IrreducibleIsKeptAndSaysWhy`,
  `schema.TestDynamicAnchors_CountsWhatAnAliasBringsIn`,
  `schema.TestDynamicAnchorIndex_ReportsATruncatedWalk`

### `content-vocabulary`

`contentEncoding`/`contentMediaType`/`contentSchema` → `Encoding` on the scalar the position lowers
to — the last as `Encoding.Schema`, a TypeRef to the decoded shape hoisted at its own pointer — and
all three → Unmodeled (`no_ir_home`) at a position with no `Encoding` field, per §4.7

The content vocabulary is modelled, not preserved. All three keywords share one home, so a
position keeps them all or lowers them all: `contentEncoding` to `Encoding.Name`,
`contentMediaType` to `Encoding.MediaType`, and `contentSchema` to `Encoding.Schema` (§5.3) on
the scalar the position lowers to. `contentSchema`'s value is a schema and lowers like one —
hoisted at its own source pointer and referenced by ID — rather than riding along as a payload
a consumer would have to re-parse. Where the position lowers to a shape with no `Encoding`
field, all three stay verbatim under `ReasonNoIRHome`.

- Cases: `content-vocabulary`
- Codes: `openapi/degraded-construct`
- Tests: `schema.TestContentVocabulary_LowersToEncoding`

### `dialect-keywords`

`$id`/`$schema`/`$vocabulary` → Unmodeled (`out_of_scope`, `$id` not honoured for resolution), and
the document's `jsonSchemaDialect`, the default `$schema` of its schemas, → Document.Unmodeled the
same way, every schema read in the OpenAPI dialect whatever it names

`$id`, `$schema` and `$vocabulary` identify and configure a JSON Schema *resource*. The IR
identifies every type by a synthetic ID derived from its source pointer rather than by `$id`
(§3), and describes one API surface rather than a resource graph, so it has no dialect axis and
none is coming: they are kept verbatim under `ReasonOutOfScope`, and `$id` is explicitly *not*
honoured as a base URI for reference resolution. An OpenAPI document's `jsonSchemaDialect`, the
default `$schema` of its schemas, is kept on the document the same way. `$comment` is dropped
outright — 2020-12 §8.3 forbids presenting it to end users, so no emitter may see it.
`$dynamicAnchor` is consumed as a reference target rather than carried as content — it is what lets
a `$dynamicRef` expand. `$anchor` is currently neither: Milestone 1 resolves same-document JSON
pointers only, so a `$ref` naming an anchor is reported unresolved rather than resolving through it
(GitHub #141). It is excluded here because the compiler does nothing with it today, not because
dropping it is settled.

- Cases: `dialect-keywords`
- Codes: `openapi/degraded-construct`, `openapi/unresolved-ref`
- Tests: `schema.TestDialectKeywords_KeptOutOfScope`, `openapi.TestMeta_JSONSchemaDialectIsKept`

### `vocabulary-coverage`

The keyword lists of §4.7 and of this section are held to the compiler rather than merely asserted.
The OpenAPI compiler's `TestVocabulary2020_12_EveryKeywordIsLoweredOrKept` enumerates the 2020-12
vocabularies keyword by keyword and, for each, compiles three documents — the keyword absent,
written with one value, written with a second — requiring that the absent form differs from the
first and the first from the second. So it holds both the keyword's *presence* and its *value* to
reaching the compiled IR, by lowering or by verbatim preservation; a keyword that stops being
carried fails there rather than waiting for a reader to notice.

Two limits are worth stating, because the guarantee is a floor rather than an inventory. It
compares one pair of values per keyword, so a lowering that carried one spelling and dropped
another would still pass. And rows that legitimately change nothing assert the inverse and carry
a written justification — `$comment`, which the spec forbids surfacing; `$defs`, whose members
lower on demand through a `$ref`; `$dynamicAnchor`, read as a reference target; and `$anchor`,
which the compiler does not yet honour.

- Tests: `schema.TestVocabulary2020_12_EveryKeywordIsLoweredOrKept`

## 7. Paths and operations

### `path-keys`

a Paths Object key → HTTPBinding.URITemplate as written, and one that breaks OpenAPI's path
templating rules (3.2.0 §4.8.2, read at every version) lowered as declared + `invalid-path-key`
warning naming every rule it breaks, a `#` and what follows it left to GitHub #602

- Cases: `http-binding`
- Codes: `openapi/invalid-path-key`
- Tests: `operation.TestPathKeys_MalformedKeyIsReportedOnce`,
  `operation.TestPathKeys_RulesAreNotVersionGated`,
  `operation.TestPathKeys_FragmentIsLeftToGitHub602`

### `path-item-operations`

every operation a path item declares — the fixed method fields, 3.2 `query`, and 3.2
`additionalOperations` keyed by method — → an Operation apiece, mounted at its own pointer, with the
`additionalOperations` key used verbatim as HTTPBinding.Method since OpenAPI reads a method name
case-sensitively, and a key naming no method at all lowered as declared + `invalid-method-key`
warning (the binding is unusable, but dropping the entry would lose every operation it declares)

- Cases: `path-item-operations`, `later-version-constructs`
- Codes: `openapi/invalid-method-key`
- Tests: `operation.TestPathItem_EmptyAdditionalOperationsKeyReported`,
  `operation.TestPathItem_AdditionalOperationsLowerOnEveryRoute`,
  `operation.TestPathItem_AdditionalOperationsBindCallbacks`

### `operation-ids`

an `operationId` → `Operation.Name` as `Source` and its neutral `Canonical` ([`naming`](#naming)),
while the `OpID` is derived from the operation's mount pointer (§3.1) and never from the
`operationId`, so a repeated or renamed id moves no ID. An operation with none, or with `""`, claims
no id and is named by a hint of its method and the route's key in neutral words — the path template,
a webhook's name, a callback's expression, or an `additionalOperations` key standing as the method
(`get_ping`, `post_new_pet`; §8.1). Uniqueness is judged once every operation has lowered, across
paths, webhooks and callbacks alike, and by declaration, so a finding does not move with the order
the document is lowered in: a second declaration writing a claimed id →
`openapi/conflicting-operation-id` error at the pointer it is written at, naming the first
declaration, the declarations taken in pointer order; one declaration mounted more than once, by a
path item `$ref`, a YAML alias or a merge key → `openapi/duplicate-operation-id` warning at each
mount but the first, naming that mount. Every operation lowers whatever the verdict, repeated names
included. The parser's own uniqueness check is dropped for this one.

- Codes: `openapi/duplicate-operation-id`, `openapi/conflicting-operation-id`
- Tests: `operation.TestOperations_DuplicateOperationIDReported`,
  `operation.TestJudge_ReportsEveryDeclarationAndMountButTheFirst`,
  `operation.TestOperation_NoOperationIdHint`

### `path-item-servers`

path-item `servers`, under `paths`, `webhooks` and a callback expression alike → Operation.Unmodeled
(`no_ir_home`: §10 scopes servers by index list at service and channel, and an operation has no such
list yet), with an operation's own `servers` — which OpenAPI says override the path item's — kept
beside them under `openapi:operationServers`, the one key here not named for the keyword it holds,
since two declarations at two pointers cannot share one map key without the survivor depending on
lowering order. A path item that mounts no operation keeps its `servers` and `x-*` names on the
service instead, or on the declaring operation's HTTP binding for a callback expression, under
`openapi:pathItem/<item pointer>/<key>`, the item's pointer in the key since one carrier holds every
such item, + one `degraded-construct` warning at the mount

- Cases: `webhooks`, `callbacks`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperations_PathItemServersKeptOnEveryRoute`,
  `operation.TestOperations_OwnServersKeptBesideThePathItems`,
  `operation.TestOperations_ServersKeysAreIndependent`

### `path-item-docs`

a path item's own `summary`/`description`, at the same three mounts → Operation.Unmodeled under
`openapi:pathItemSummary`/`openapi:pathItemDescription` (`no_ir_home`) rather than merged into Docs:
ir.Docs holds the operation's own pair and a path item's documents the path, so merging would need a
precedence rule and would attach documentation the operation's author never wrote — an inference,
which invariant 6 places in policy rather than in a lowering

- Cases: `path-item-docs`, `path-item-ref-siblings`
- Codes: `openapi/degraded-construct`
- Status: partly built: a path item that mounts no operation drops its own summary and description
  in silence (#383).

### `path-item-ref-fields`

the fields a Path Item Object writes beside its `$ref`, which OpenAPI allows and leaves undefined
only where both sides declare one field, → a layer of their own at their own pointers, a mount
reading the fields beside each `$ref` of its chain nearest the use site first and then the item the
chain ends at, the nearer layer keeping every method or `additionalOperations` key, `summary`,
`description`, `servers`, `x-*` name, undeclared key and item `(name, in)` parameter both declare
and the other reported (`path-item-ref-collision`, warning) rather than merged, so no construct is
attributed to a declaration that did not write it, and the mount listing its operations in the order
one item holding them all would; a reference whose pointer passes through those fields — which the
resolver reads through the `$ref` into the item it names, while the mount declares the field written
there at that very pointer — is left unresolved and reported (`unresolved-ref`) rather than lowered
as either, a `#/$defs/...` pointer written in them included, since its definition is read the same
way, unless what the resolver found there is the field written there, as it can be for a reference
other than a schema's `$ref` or a mapping value when external references are allowed (GitHub #824);
an item another document holds, mounted where the use site writes fields too, is declared under the
use site's `$ref` (`…/$ref/…`), a position the source cannot name, and the fields beside a `$ref`
the load phase's walk does not reach, in another document or under an `x-*` key, are reported
(`degraded-construct`) rather than lowered

- Cases: `path-item-ref-siblings`
- Codes: `openapi/path-item-ref-collision`, `openapi/unresolved-ref`, `openapi/degraded-construct`
- Tests: `operation.TestPathItemRefSiblings_CollisionIsLoudAndUseSiteWins`,
  `operation.TestPathItemRefSiblings_AReferenceIntoTheFieldsBesideARefIsRefused`,
  `operation.TestPathItemRefSiblings_FieldsAnotherDocumentWritesAreReported`

### `tag-groups`

tags (3.2 parent/kind) → groups + TagDefs

**How a tag builds a group.** Two facts decide it, and both are declared rather than inferred. The
group comes from the operation's first *navigational* tag, not simply its first: OpenAPI 3.2 gives a
tag a `kind`, and the only kind that groups is the registry's navigational value (`nav`), with a tag
that declares no kind — as a 3.0 or 3.1 tag does — navigational too. Any other declared kind is
skipped, whether or not the registry names it; reading a section out of an unregistered string would
be an inference (invariant 6), and no diagnostic is reported because the document loses nothing —
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
did before.

- Cases: `tags-grouping`, `tags-grouping-32`, `group-identity`
- Tests: `operation.TestGrouping_ParentCycleNestsNoMemberUnderAnother`,
  `operation.TestGrouping_DanglingParentStaysTopLevel`,
  `operation.TestGrouping_BadgeOnlyOperationFallsToDefault`

## 8. Parameters

### `parameters`

parameters → Params + HTTPBinding locations w/ style/explode

- Cases: `param-styles`, `param-style-matrix`, `param-identity`
- Codes: `openapi/duplicate-parameter`
- Tests: `operation.TestParams_RepeatedNameAndLocationKeepsEveryParameter`

### `content-parameters`

a parameter or header that states its type with `content` instead of `schema` → the first media-type
entry, which OpenAPI requires to be the only one. Its schema is the type, lowered as the `schema`
spelling's is with the same constraints and `xml` hints, hoisted at `<owner>/content/<key>/schema`
or the top type where the entry writes none, and the key as written is the media type that
serializes the value: `HTTPParamBinding.ContentType` on a parameter, `Property.Encoding.MediaType`
on a header, neither set by the `schema` spelling. Further entries lower nothing: a
`degraded-construct` warning at the `content` map names them. What else the entry writes is kept:
its `example` and `examples` follow the parameter's or header's own in `Examples`, and its `x-*`
extensions, `description`, undeclared keys and the fields only a body Content models — `itemSchema`,
`itemEncoding`, `prefixEncoding` and `encoding`, each with an info `degraded-construct` — go
verbatim under `openapi:content/<key>/<field>`, the key escaped as a pointer segment, as
`vendor_extension` or `no_ir_home`. A `content` map with no entry elects nothing: a `schema` beside
it stands and the empty map is kept (`degraded_lowering`) with a warning. The converse, `schema`
beside a non-empty `content`, is [`competing-keywords`](#competing-keywords)'s.

- Cases: `param-content-fields`, `header-content-schema`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_ContentStyleParameter`,
  `operation.TestSingleContentEntry_ReportsExtraMediaTypes`,
  `operation.TestParamContent_OwnAndMediaExamplesAreBothKept`

### `allow-empty-value`

`allowEmptyValue` → Parameter.Unmodeled (`no_ir_home`: HTTPParamBinding holds its neighbours but not
this one)

- Cases: `param-styles`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_AllowEmptyValueKept`

### `reserved-header-names`

a header parameter named Accept/Content-Type/Authorization lowered as declared +
`reserved-header-name` warning (OpenAPI says such a definition SHALL be ignored; dropping declared
content is an emitter's call, not a compiler's)

- Codes: `openapi/reserved-header-name`
- Tests: `operation.TestParams_ReservedHeaderNamesAreReported`

### `querystring`

3.2 `in: querystring` → querystring location; a declared `style` there lowers as declared, reported
as an error by the parser's validation, while `explode` or `allowReserved` (which it does not check;
GitHub #408) lowers as declared + `invalid-location-keyword` warning at the keyword's own pointer

- Cases: `param-querystring`, `querystring-forbidden-keywords`
- Codes: `openapi/invalid-location-keyword`, `openapi/validation`
- Tests: `operation.TestParams_QueryStringDeclaredStyleIsKeptAndReported`

### `allow-reserved-locations`

an `allowReserved` its document's version gives no effect — at `path`, `header` or `cookie` before
3.2, which applies it to `in: query` alone, and at `header` or a `cookie` in `style: cookie` in 3.2,
which applies it wherever the value is percent-encoded — lowers as declared + an
`inapplicable-location-keyword` warning at the keyword, a code of its own since no MUST is broken,
reported once where the parameter is written, whether anything lowers it or not (a parameter another
document holds is not judged; GitHub #839)

- Cases: `param-allowreserved-locations`
- Codes: `openapi/inapplicable-location-keyword`
- Tests: `load.TestReservedIgnored_FollowsEachMinorsText`,
  `load.TestReservedFinding_ReportsWhereTheVersionGivesNoEffect`,
  `load.TestReservedFinding_AnotherDocumentIsNotJudgedByTheRoots`

### `parameter-schema-keywords`

a parameter schema's `xml` and its `readOnly`/`writeOnly` → Parameter.Unmodeled (`no_ir_home`:
Parameter has no field for either), except the `xml` of a schema that owns a node of its own, which
stays on that node's XMLHints

- Cases: `param-xml-residue`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestParams_SchemaVisibilityKeptUnderUnmodeled`,
  `operation.TestParams_SchemaVisibilityKeptWhenTheSchemaOwnsANode`,
  `operation.TestParams_SchemaXMLHintsStayOnAnOwnedNode`

## 9. Request bodies and content

### `media-types`

every media type of a `content` map → one `Content` apiece, in source order and with none elected
primary (§7.2). `Content.MediaType` is the key as written — case, parameters and wildcards kept, so
`application/json` and `Application/JSON` are two Contents — and its `Type` is the media type's
`schema`, hoisted at `<owner>/content/<key>/schema` with the key escaped as a pointer token, or the
top type where it writes none. A request body's keys also go, as written and in order, to
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

### `request-body`

requestBody/responses all content types → Payload.Contents, `requestBody.required` →
Payload.Required, always set since OpenAPI's own default makes an undeclared `required` mean false
rather than unstated, and `requestBody.description` → Payload.Docs, a request body whose `content`
map is empty — which every minor's schema admits and 3.2 says SHOULD hold an entry — lowering to a
Payload with no Contents that keeps all three, + `empty-request-content` warning at the map

- Cases: `empty-request-content`, `request-body-docs`
- Codes: `openapi/empty-request-content`
- Tests: `operation.TestContent_NonRequiredRequestBody`,
  `operation.TestLowerRequestBody_AnEmptyContentKeepsTheBody`

### `file-bodies`

a content entry whose schema is `type: string` with `format: binary`, read through a `$ref` and
whatever its media type, or whose schema is absent or boolean on `application/octet-stream` (matched
on the normalized key, [`media-types`](#media-types)) → `Content.File` with `IsText` false and
`ContentTypes` the key as written, and `Content.Type` the non-nullable `bytes` primitive in place of
whatever the schema lowered to, so that a body to stream is not read as a model. No other `FileInfo`
field is set. The same holds for a response and an error case. An `octet-stream` entry with a schema
that is not `string`+`binary`, a `format: byte` or `contentMediaType` string, and a schema-less
entry on any other media type are ordinary Contents.

- Cases: `file-body`
- Tests: `operation.TestContent_BinaryOctetStreamBody`,
  `operation.TestContent_MixedCaseOctetStreamBodyIsFile`,
  `operation.TestContent_BinaryRefBodyDetectedAsFile`
- Status: partly built: a binary body's schema keeps its docs and bounds on a node no Content
  references (#600), and the 3.1 idioms for a file, `contentMediaType` without `contentEncoding` and
  a schema-less entry on a media type other than `application/octet-stream`, are not recognized
  (#605).

### `example-values`

3.2 example `dataValue` → Example.Value and `serializedValue` → Example.Unmodeled (`no_ir_home`)

- Cases: `examples-32`
- Codes: `openapi/degraded-construct`

### `item-schema`

3.2 `itemSchema` → Content.Item and `itemEncoding` → Content.ItemEncoding — except beside a
positional `prefixEncoding`, where `itemEncoding` and `prefixEncoding` go to Content.Unmodeled
(`no_ir_home`), because a single every-item encoding cannot state ordinals, and `itemSchema` still
lowers to Content.Item

- Cases: `sequential-media`
- Codes: `openapi/degraded-construct`, `openapi/unpreservable-construct`

### `streaming-media`

a request body or success response with a Content whose media type the streaming policy names —
`application/jsonl`, `application/x-ndjson` and `text/event-stream` by default, matched on the
normalized key ([`media-types`](#media-types)) at every minor → `Operation.RequestStream` and
`ResponseStream` for the directions that have one, each with `Events` the Content's own `Type` (for
a frame format the schema describes one frame, not the body), `Streaming` the `client`, `server` or
`bidi` summary of them, and `Provenance.Inferred` the marker `streaming-media-type` on the
operation, joined by `,` to any other marker, since below 3.2 a media type is a guess about framing
and not a declaration. A 3.2 `itemSchema` ([`item-schema`](#item-schema)) is a declaration: it
streams on any media type with `Events` the `Item` type and no marker, and wins over the media-type
reading at its own Content. Streaming Contents that name different element types leave `Events`
unset on a direction that still streams, every Content kept, with an info `degraded-construct` at
the `requestBody` or `responses` pointer; Contents that name the same element elect it. Error
responses are not read. The policy is the Go option `Options.StreamingMedia`: `Disabled` turns the
media-type reading off, leaving only what an `itemSchema` declares, and `MediaTypes` replaces the
default list instead of extending it.

- Cases: `streaming-media-30`, `streaming-media-31`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestStreaming_ResponseMediaTypeImpliesServerStream`,
  `operation.TestStreaming_SeveralStreamingContentsLeaveTheElementUnnamed`,
  `operation.TestStreaming_ItemSchemaDeclaresTheStream`
- Status: partly built: an operation that does not stream carries an empty `Streaming`, where §7.2
  declares `none` (#978).

### `multipart-encoding`

an Encoding Object of a `multipart/*` or `application/x-www-form-urlencoded` Content → a
`PartEncoding` in `Content.Encoding`, keyed by the `PropID` of the body property its key names,
wherever the body declares that property: on the schema, in an `allOf` branch with the first
declaration of a name winning, or on a `Base` or `Mixin` it composes. `contentType` →
`ContentTypes`, split on `,` and trimmed with empty items dropped; `headers` → `Headers`, lowered as
a response's are; `style` and `explode` → `Style` and `Explode` as declared, nothing defaulted.
`Multi` and `Filename` come from the part's own schema and never from the `encoding` map: `Multi`
where it is an array, `Filename` where it is a `string` with `format` `binary` or `byte`. A part is
entered only when its `PartEncoding` holds something, so a body that declares no encoding still gets
entries for its array and file parts, and an entry declaring nothing leaves none. What an Encoding
Object writes beyond those fields is [`encoding-allow-reserved`](#encoding-allow-reserved)'s.

- Cases: `multipart-encoding`
- Tests: `operation.TestContent_MultipartPartEncoding`,
  `operation.TestContent_ArrayMultipartPartMulti`,
  `operation.TestPartEncodings_MixinPartIsKeyedOnTheMixin`
- Status: partly built: an `encoding` key that names no property, or one declared only inside a
  `oneOf`/`anyOf` branch, is dropped with no diagnostic (#604, #744), and an array of files is not
  marked as files (#605).

### `encoding-allow-reserved`

an Encoding Object's `allowReserved` → Content.Unmodeled (`no_ir_home`) under
`openapi:encoding/<part>/allowReserved` or `openapi:itemEncoding/allowReserved`, ir.PartEncoding
holding `style` and `explode` beside it but no field for this one, and read before the entry's
emptiness is judged so an entry declaring nothing else is not dropped with the empty PartEncoding it
lowers to

- Cases: `multipart-encoding`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding`,
  `operation.TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey`

## 10. Responses and errors

### `response-conditions`

per-status responses/default → Conditions + ranges, with the responses-map key as declared and then
neutralized → Response.Name.Hint and ErrorCase.Name.Hint alike (`404`, `5_xx`, `default`), which
records the spelling a range cannot state, though neutralizing a range key folds its case (`5XX` and
`5xx` both → `5_xx`)

- Cases: `per-status-errors`
- Codes: `openapi/invalid-status-key`
- Tests: `operation.TestResponses_NamedByStatusKey`,
  `operation.TestResponses_InvalidStatusKeyIsReported`

### `duplicate-status-key`

two keys resolving to one range — `4XX` beside `4xx` — are both kept and reported
`openapi/duplicate-status-key`, since they reach the IR with one name and one condition

- Codes: `openapi/duplicate-status-key`
- Tests: `operation.TestResponses_TwoKeysForOneRangeAreReported`,
  `operation.TestResponses_ACollisionIsReportedTheSameInEitherOrder`,
  `operation.TestResponses_StatusKeyDiagsAreSitedAtTheOperation`

### `error-responses`

an error response lowers exactly as a success one — its `headers` → ErrorCase.Headers and every
media type of its `content` → ErrorCase.Payload.Contents, neither degraded and neither kept under
Unmodeled, and the payload's naming hint derived from the declaration pointer on both sides so that
one `components/responses` entry mounted at a success and an error status interns one type whichever
side reaches it first

- Cases: `per-status-errors`, `shared-response-across-status`

### `response-summary`

3.2 Response `summary` → Docs.Summary

- Cases: `response-summary-32`
- Codes: `openapi/later-version-construct`, `openapi/degraded-construct`,
  `openapi/unknown-key-unreachable`
- Tests: `operation.TestResponses_SummaryIsReadAtEveryVersion`,
  `operation.TestResponses_SummaryNoReaderTakesIsStillReported`

### `header-style`

response/encoding header `style` and `explode` → Property.Unmodeled (`no_ir_home`, ir.Property has
neither field), and a `Content-Type` entry in either headers map lowered as declared + the same
`reserved-header-name` warning the parameter position gets

- Cases: `header-content-schema`
- Codes: `openapi/degraded-construct`, `openapi/reserved-header-name`
- Tests: `operation.TestHeaders_SerializationKeywordsKept`,
  `operation.TestHeaders_ReservedContentTypeEntryIsReported`,
  `operation.TestHeaders_ReservedContentTypeInEncodingIsReported`

### `links`

links → Response.Unmodeled and ErrorCase.Unmodeled alike (`no_ir_home`, promotable later): the two
are lowerings of one Response Object, so a construct kept on only the success one makes a
declaration survive or vanish on nothing but its status code

- Cases: `response-links`

## 11. Webhooks and callbacks

### `webhooks`

webhooks → HTTPBinding.IsWebhook, the map key → HTTPBinding.WebhookName with URITemplate left empty,
since the key names an event rather than a path

- Cases: `webhooks`

### `callbacks`

callbacks → Callbacks

- Cases: `callbacks`
- Tests: `operation.TestCallbacks_NameWithSlashKeepsItsOwnKey`
- Status: partly built: callbacks declared on an operation inside a callback are dropped in silence
  (#858).

## 12. Security

### `security-requirements`

securitySchemes/security → Auth OR-of-ANDs, 3.2 device flow + `oauth2MetadataUrl` →
Flows/OAuth2MetadataURL, and an oauth2 scheme whose `flows` declares no flow and that has no
`oauth2MetadataUrl` lowered as declared + `oauth2-no-flow` warning, a `flows` the parser's
REQUIRED-field rule finds missing being that rule's to report instead

- Cases: `security-schemes`, `security-or-and`, `oauth2-no-flow`
- Codes: `openapi/oauth2-no-flow`, `openapi/incomplete-security-scheme`, `openapi/unresolved-ref`
- Tests: `auth.TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration`,
  `auth.TestLowerSecuritySchemes_AnEntryNamingNoMechanismIsRefused`,
  `auth.TestAuth_RequirementsOrOfAnds`

### `oauth-flow-fields`

a flow field the flow's kind does not define (`authorizationUrl` beside a token-only flow,
`tokenUrl` on `implicit`, `deviceAuthorizationUrl` off the device flow) kept verbatim in that flow's
Unmodeled (`degraded_lowering`) + info, never lowered into a field the flow lacks

- Cases: `oauth-flow-fields`
- Codes: `openapi/degraded-construct`

## 13. Servers

### `servers`

servers+variables (3.2 named) → Servers, with a name one servers list repeats — the document's, a
path item's or an operation's, compared as written on every version, since the parser reads `name`
on each — kept as declared on both entries — the document's in Servers, the others in the lists
[`path-item-servers`](#path-item-servers) keeps — + `duplicate-server-name` warning

- Cases: `servers-variables`
- Codes: `openapi/duplicate-server-name`
- Tests: `openapi.TestDuplicateServerName_BothServersLower`,
  `load.TestServerRepeats_ReportsEveryListTheSourceWrites`,
  `load.TestServerRepeats_ReportsOnlyANameOneListRepeatsExactly`

## 14. Vendor extensions and policy

### `vendor-extensions`

`x-*` → namespaced Unmodeled (legal on every object — hence Unmodeled on every node), read at every
object that admits one: an object lowering to a node with a map of its own keeps them unscoped
there, and one lowering to no node of its own is keyed by the path from its carrier down to it — on
the document, `openapi:info/x-*`, `openapi:info/contact/x-*`, `openapi:info/license/x-*`,
`openapi:externalDocs/x-*`, `openapi:components/x-*`, `openapi:tags/<i>/x-*`,
`openapi:tags/<i>/externalDocs/x-*`; on the service, `openapi:paths/x-*`; on each operation the path
item's `openapi:pathItem/x-*` plus `openapi:responses/x-*` and `openapi:externalDocs/x-*`; on the
HTTP binding, `openapi:callbacks/<name>/x-*`; on the content, `openapi:encoding/<part>/x-*` and
`openapi:itemEncoding/x-*`, `<part>` and `<name>` alike escaped RFC 6901 style so a document-chosen
name stays one segment (§12); on the schema's type, `openapi:xml/x-*`, `openapi:discriminator/x-*`,
`openapi:externalDocs/x-*`; on the scheme, `openapi:flows/x-*` — since several such objects reach
one map and an unscoped key would leave the survivor to lowering order (§12); a Link Object's own
ride inside the verbatim `links` entry rather than taking a key beside it; a path item that mounts
no operation keeps its own under `openapi:pathItem/<item pointer>/x-*`, as
[`path-item-servers`](#path-item-servers) says; an extension whose value cannot be serialized is
reported (`degraded-construct`, warning) and reaches the IR in no form

- Cases: `extensions-x`, `inline-annotations`
- Codes: `openapi/degraded-construct`
- Tests: `operation.TestOperation_UnserializableExtensionStillWarns`,
  `operation.TestEncoding_PartNameWithSlashKeepsItsOwnKey`

### `unknown-keys`

a key an OpenAPI object neither defines nor admits as an `x-` extension → kept verbatim under
`openapi:<key>` in the `Unmodeled` of the node that carries the object, `out_of_scope`, + an
`unknown-object-key` warning at the key's own pointer, since the object's key set is closed and such
a key is usually a misspelling of its neighbour; an object that lowers to no node of its own is
keyed beneath the scope its extensions use (`openapi:info/<key>`, `openapi:pathItem/<key>`,
[`vendor-extensions`](#vendor-extensions)). A path item's is judged like any other, a value carrying
a YAML anchor included. A schema keyword no field of the schema model names → the same entry + an
`unknown-schema-keyword` info, because JSON Schema obliges an implementation to ignore a keyword it
does not know; `$comment`, `$dynamicAnchor` and `$dynamicRef` are decided elsewhere and left out.
One object contributes at most 64 keys, taken in byte order after the keys another reader already
kept at the same pointer are set aside, and the rest reach the IR in no form, with one
`unknown-key-budget` warning at the object. Two other outcomes also keep nothing and warn:
`unknown-key-entry-taken`, where `openapi:<key>` is already held by a construct written elsewhere on
a carrier two objects share, a parameter's own key and its schema's keyword, whichever lowered first
surviving; and `unknown-key-unreachable`, where the key is merged in through `<<` and the raw
mapping holds no pair for it.

- Cases: `path-item-anchored-key`
- Codes: `openapi/unknown-object-key`, `openapi/unknown-schema-keyword`,
  `openapi/unknown-key-budget`, `openapi/unknown-key-entry-taken`, `openapi/unknown-key-unreachable`
- Tests: `openapi.TestUnknownKeys_KeptAtEveryObject`,
  `openapi.TestUnknownKeys_SchemaAndObjectAreGradedApart`,
  `annotation.TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes`
- Status: partly built: a parameter's key and its schema's keyword of one spelling share an entry
  (#396), and a key merged in through `<<` is reported and kept nowhere (#395).

### `extension-promotion`

A value the mapped field cannot hold — anything but text, for the four `Deprecation` members — is
reported and not coerced, since the document means something else by the key. Text of the right
JSON shape is taken as written: `x-sunset` fills `RemovalDate` and not `RemovalVersion` because
the header it echoes ([RFC 8594](https://www.rfc-editor.org/rfc/rfc8594)) is a date by
definition, and the mapping is where that reading is stated — the promotion does not then parse
the date to confirm it. No default key names `RemovalVersion`: a document stating a removal
*version* names its own key, per §12's rule 1.

`Enum.Closed` is the one target whose fact is stated by a key being *present* rather than by a
value, so nothing is read or reported there. Its default key, `x-extensible-enum`, writes the
member list as its own value, and a list of members says nothing about openness that the key
naming it has not already said; the promotion therefore clears `Closed` on presence. A boolean
value is the one shape that does state openness by itself, and an explicit `false` is read as
written rather than inverted. Only openness is ever promoted: a schema's `enum` is closed by
definition, so a document declares the open case or nothing, and the mapping names that fact
rather than the field's own polarity. A document that writes `x-extensible-enum` *instead* of
`enum` lowers to no `Enum` at all and there is no node to open — minting one from a vendor key
would be a compiler reading a member list out of an extension, not a promotion, so the entry is
left for a consumer that wants to.

- Cases: `extension-promotion`
- Codes: `openapi/degraded-construct`
- Tests: `lowering.TestPromoteDeprecation_ValueThatIsNotTextIsReported`,
  `lowering.TestPromoteEnumOpenness_WritesNothing`,
  `openapi.TestPromotion_RemovalDateAndVersionAreSeparateFacts`

### `mutually-exclusive-parameters`

mutually-exclusive parameter groups (`x-mutually-exclusive-parameter-groups`) stay as namespaced
Unmodeled entries, and their documented *promotion* (no dedicated node needed) is a pass that
synthesizes one logical `Parameter` typed by a `Union` of variant models, bound via
`HTTPParamBinding.ParamPath` per field

- Status: partly built: the groups are kept as any extension is, and no pass promotes them yet
  (#988).

### `pagination-policy`

pagination only via injectable policy, marked Inferred

- Status: not built: no policy infers pagination yet, so the compiler sets no Pagination (#989).

## 15. Swagger 2.0

### `swagger-lift`

lifted to OpenAPI 3.x shape first (body/formData → Payload; host/basePath/schemes → Servers;
consumes/produces → content types), then the OpenAPI lowering runs

- Status: not built: a Swagger 2.0 document is recognized and refused with
  engine/no-compiler-for-format until milestone 2.
