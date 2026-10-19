# OpenAPI compiler — lowering specification

This document is normative for `compilers/openapi`, as [`ir-design.md`](../ir-design.md) is for
the IR: it states how a construct of an OpenAPI document lowers into the IR. The IR's shapes, and
what a consumer may rely on, are ir-design's. A bare section number (`§4.3`) names a section of
ir-design.

## Rules

- components/schemas → registry (IDs from pointers)
- inline schemas hoisted with hints
- `allOf` → Base/Mixins per §4.3
- `oneOf`/`anyOf` → Union (Exclusive bit), null-variant → Nullable ref (a keyword beside the
  collapsed union that no reference holds — a `discriminator`, a `format` other than `password`, a
  collection bound — kept verbatim Unmodeled, `degraded_lowering`, on the node or carrier owning the
  position, as at a `$ref` site), a null-only branch set (every branch a bare `type: null`) → the
  same nullable `any` a bare `{type: null}` schema at that position lowers to, with the oneOf/anyOf
  itself kept verbatim Unmodeled (`degraded_lowering`) since no node built for it carries a branch
  set, co-declared with structural keywords → the composition distributed across the variants per
  §4.3, or — for the five shapes that cannot be distributed — structural body + verbatim union per
  §4.8 (branches that declare no shape at all are `validation_only` per §4.7), with a declared
  `discriminator` beside it staying on the model only when every type it routes to is a subtype of
  it, decided once every node is interned, and otherwise kept verbatim in
  `Unmodeled["openapi:discriminator"]` (§4.8), a body the move leaves a map and nothing else
  becoming its `MapT` (§4.6)
- the same co-declared `oneOf`/`anyOf` beside a `$ref` at the same level, rather than a structural
  body, has nothing at that position to distribute the union across either, so it lowers to the
  alias over the `$ref` target with the union kept verbatim beside it (`degraded_lowering`), through
  the same keeper as the structural-body case
- an `allOf` beside a `$ref` at the same level joins the alias's unhomed-keyword census the same way
  (`degraded_lowering`), since a `$ref` site elects no composition family for it to lose or win
- competing keywords at one position — `const`/`enum`/`allOf`, elected in that order
- `oneOf` beside `anyOf`, where oneOf wins
- and a parameter's or header's `schema` beside its `content`, where content wins, since a
  media-type entry names both a schema and the media type serializing it and the IR models both —
  lower as the elected keyword with every passed-over one verbatim Unmodeled (`degraded_lowering`)
  at its own pointer per §4.8, and a `{X, null}` oneOf beside an anyOf stays a Union rather than
  collapsing to a nullable ref
- `discriminator` → Discriminator (3.2 `defaultMapping` → Discriminator.Default), beside a node that
  does not route by it (neither a Model nor a Union) → verbatim Unmodeled (`degraded_lowering`) per
  §4.8 (a multi-typed schema excepted, which runs no census yet, #852), and a subtype the mapping
  names under several keys — an alias tag — takes the smallest in byte order as its
  DiscriminatorValue, since the field holds one and a mapping's key order is not a property of the
  document, with every key kept in the base's Mapping and the election reported
  (`degraded-construct`, info)
- `enum` → Enum at any member count, or a Union of Literals where an Enum cannot hold its members,
  and `const` → Literal (§4.5)
- `nullable`/type-arrays → Nullable
- readOnly/writeOnly → Visibility and schema-level `default` → the referencing Property/Parameter's
  Default: both bind a *use* of the type rather than the type, so a declaration-site one is pushed
  down to referencing properties with use-site precedence and the declaration keeps its own copy
  verbatim (`no_ir_home` Unmodeled + diagnostic) — which is what a component nothing references
  would otherwise lose silently
- `additionalProperties: false` → Additional=closed, `additionalProperties: true` or a schema →
  AdditionalProps (`true` typed as the top type, as `{}` is), and an object whose one member fact
  that catch-all is → a `MapT` per §4.6, `unevaluatedProperties: false` → closed_after_composition,
  `minProperties`/`maxProperties` → Model.Constraints (the property set's cardinality, as against
  Additional's openness) or MapT.Constraints (its entry count)
- parameters → Params + HTTPBinding locations w/ style/explode, `allowEmptyValue` →
  Parameter.Unmodeled (`no_ir_home`: HTTPParamBinding holds its neighbours but not this one), a
  header parameter named Accept/Content-Type/Authorization lowered as declared +
  `reserved-header-name` warning (OpenAPI says such a definition SHALL be ignored; dropping declared
  content is an emitter's call, not a compiler's), 3.2 `in: querystring` → querystring location
- a declared `style` there is refused by the parser, while `explode` or `allowReserved` (which it
  does not check; GitHub #408) lowers as declared + `invalid-location-keyword` warning at the
  keyword's own pointer
- an `allowReserved` its document's version gives no effect — at `path`, `header` or `cookie` before
  3.2, which applies it to `in: query` alone, and at `header` or a `cookie` in `style: cookie` in
  3.2, which applies it wherever the value is percent-encoded — lowers as declared + an
  `inapplicable-location-keyword` warning at the keyword, a code of its own since no MUST is broken,
  reported once where the parameter is written, whether anything lowers it or not (a parameter
  another document holds is not judged; GitHub #839)
- requestBody/responses all content types → Payload.Contents, `requestBody.required` →
  Payload.Required, always set since OpenAPI's own default makes an undeclared `required` mean false
  rather than unstated, and `requestBody.description` → Payload.Docs, a request body whose `content`
  map is empty — which every minor's schema admits and 3.2 says SHOULD hold an entry — lowering to a
  Payload with no Contents that keeps all three, + `empty-request-content` warning at the map
- 3.2 Response `summary` → Docs.Summary
- 3.2 example `dataValue` → Example.Value and `serializedValue` → Example.Unmodeled (`no_ir_home`)
- 3.2 `itemSchema` → Content.Item and `itemEncoding` → Content.ItemEncoding — except beside a
  positional `prefixEncoding`, where both go to Content.Unmodeled (`no_ir_home`) because a single
  every-item encoding cannot state ordinals
- an Encoding Object's `allowReserved` → Content.Unmodeled (`no_ir_home`) under
  `openapi:encoding/<part>/allowReserved` or `openapi:itemEncoding/allowReserved`, ir.PartEncoding
  holding `style` and `explode` beside it but no field for this one, and read before the entry's
  emptiness is judged so an entry declaring nothing else is not dropped with the empty PartEncoding
  it lowers to
- per-status responses/default → Conditions + ranges, with the responses-map key as declared and
  then neutralized → Response.Name.Hint and ErrorCase.Name.Hint alike (`404`, `5_xx`, `default`),
  which records the spelling a range cannot state though only `default` survives neutralization
  unchanged
- two keys resolving to one range — `4XX` beside `4xx` — are both kept and reported
  `openapi/duplicate-status-key`, since they reach the IR with one name and one condition
- an error response lowers exactly as a success one — its `headers` → ErrorCase.Headers and every
  media type of its `content` → ErrorCase.Payload.Contents, neither degraded and neither kept under
  Unmodeled, and the payload's naming hint derived from the declaration pointer on both sides so
  that one `components/responses` entry mounted at a success and an error status interns one type
  whichever side reaches it first
- response/encoding header `style` and `explode` → Property.Unmodeled (`no_ir_home`, ir.Property has
  neither field), and a `Content-Type` entry in either headers map lowered as declared + the same
  `reserved-header-name` warning the parameter position gets
- a Paths Object key → HTTPBinding.URITemplate as written, and one that breaks OpenAPI's path
  templating rules (3.2.0 §4.8.2, read at every version) lowered as declared + `invalid-path-key`
  warning naming every rule it breaks, a `#` and what follows it left to GitHub #602
- webhooks → HTTPBinding.IsWebhook, the map key → HTTPBinding.WebhookName with URITemplate left
  empty, since the key names an event rather than a path
- callbacks → Callbacks
- links → Response.Unmodeled and ErrorCase.Unmodeled alike (`no_ir_home`, promotable later): the two
  are lowerings of one Response Object, so a construct kept on only the success one makes a
  declaration survive or vanish on nothing but its status code
- path-item `servers`, under `paths`, `webhooks` and a callback expression alike →
  Operation.Unmodeled (`no_ir_home`: §10 scopes servers by index list at service and channel, and an
  operation has no such list yet), with an operation's own `servers` — which OpenAPI says override
  the path item's — kept beside them under `openapi:operationServers`, the one key here not named
  for the keyword it holds, since two declarations at two pointers cannot share one map key without
  the survivor depending on lowering order
- a path item's own `summary`/`description`, at the same three mounts → Operation.Unmodeled under
  `openapi:pathItemSummary`/`openapi:pathItemDescription` (`no_ir_home`) rather than merged into
  Docs: ir.Docs holds the operation's own pair and a path item's documents the path, so merging
  would need a precedence rule and would attach documentation the operation's author never wrote —
  an inference, which §6 places in policy rather than in a lowering
- every operation a path item declares — the fixed method fields, 3.2 `query`, and 3.2
  `additionalOperations` keyed by method — → an Operation apiece, mounted at its own pointer, with
  the `additionalOperations` key used verbatim as HTTPBinding.Method since OpenAPI reads a method
  name case-sensitively, and a key naming no method at all lowered as declared +
  `invalid-method-key` warning (the binding is unusable, but dropping the entry would lose every
  operation it declares)
- the fields a Path Item Object writes beside its `$ref`, which OpenAPI allows and leaves undefined
  only where both sides declare one field, → a layer of their own at their own pointers, a mount
  reading the fields beside each `$ref` of its chain nearest the use site first and then the item
  the chain ends at, the nearer layer keeping every method or `additionalOperations` key, `summary`,
  `description`, `servers`, `x-*` name, undeclared key and item `(name, in)` parameter both declare
  and the other reported (`path-item-ref-collision`, warning) rather than merged, so no construct is
  attributed to a declaration that did not write it, and the mount listing its operations in the
  order one item holding them all would
- a reference whose pointer passes through those fields — which the resolver reads through the
  `$ref` into the item it names, while the mount declares the field written there at that very
  pointer — is left unresolved and reported (`unresolved-ref`) rather than lowered as either, a
  `#/$defs/...` pointer written in them included, since its definition is read the same way, unless
  what the resolver found there is the field written there, as it can be for a reference other than
  a schema's `$ref` or a mapping value when external references are allowed (GitHub #824)
- an item another document holds, mounted where the use site writes fields too, is declared under
  the use site's `$ref` (`…/$ref/…`), a position the source cannot name, and the fields beside a
  `$ref` the load phase's walk does not reach, in another document or under an `x-*` key, are
  reported (`degraded-construct`) rather than lowered
- securitySchemes/security → Auth OR-of-ANDs, 3.2 device flow + `oauth2MetadataUrl` →
  Flows/OAuth2MetadataURL, and an oauth2 scheme whose `flows` declares no flow and that has no
  `oauth2MetadataUrl` lowered as declared + `oauth2-no-flow` warning, a `flows` the parser's
  REQUIRED-field rule finds missing being that rule's to report instead
- a flow field the flow's kind does not define (`authorizationUrl` beside a token-only flow,
  `tokenUrl` on `implicit`, `deviceAuthorizationUrl` off the device flow) kept verbatim in that
  flow's Unmodeled (`degraded_lowering`) + info, never lowered into a field the flow lacks
- servers+variables (3.2 named) → Servers, with a name one servers list repeats — the document's, a
  path item's or an operation's, compared as written on every version, since the parser reads `name`
  on each — kept as declared on both entries + `duplicate-server-name` warning
- tags (3.2 parent/kind) → groups + TagDefs
- info contact/license → Document
- schema `example(s)` → Examples
- `xml` object (incl. 3.2 nodeType) → XMLHints at type and property level
- `not`/`if-then-else`/`dependentSchemas`/`dependentRequired`/`contains`/`propertyNames`/`unevaluated*`
  → verbatim Unmodeled per §4.7
- a numeric `type`+`format` → the primitive the format names — an integer width under `integer` and
  `number` alike, since OpenAPI applies a format to both (3.1.1 §4.4.1), and
  `float`/`double`/`decimal`/`decimal128` under `number` alone, since on an integer they admit only
  integral values, which no primitive narrower than `integer` holds — and a pairing no primitive
  names → a `Scalar` over the type's primitive that keeps the format, as `Encoding.Name` or, where a
  `contentEncoding` beside it takes that field, verbatim Unmodeled
- `contentEncoding`/`contentMediaType`/`contentSchema` → `Encoding` on the scalar the position
  lowers to — the last as `Encoding.Schema`, a TypeRef to the decoded shape hoisted at its own
  pointer — and all three → Unmodeled (`no_ir_home`) at a position with no `Encoding` field, per
  §4.7
- `format: password` → a redaction request rather than a (type, format) pairing, so the shape lowers
  without it and it lands where a declaration's annotations do: `Sensitive` on the node the position
  owns, whatever its kind (an alias hoisted where the shape alone would own none), or, at a property
  or header that owns none, `Property.Secret`, inherited from a `$ref` target as `deprecated` is
- at a parameter that owns none it is kept verbatim in Parameter.Unmodeled (`no_ir_home`),
  ir.Parameter having no redaction field (GitHub #578)
- `$id`/`$schema`/`$vocabulary` → Unmodeled (`out_of_scope`, `$id` not honoured for resolution), and
  the document's `jsonSchemaDialect`, the default `$schema` of its schemas, → Document.Unmodeled the
  same way, every schema read in the OpenAPI dialect whatever it names
- `$dynamicRef` → the anchored type by compiler expansion, else verbatim Unmodeled with the reason
  it was irreducible
- an inline `allOf` branch declaring more than the merge consumes → verbatim Unmodeled
  (`degraded_lowering`) per §4.8
- a property redeclared across branches with a type, constraint keyword, default, description,
  examples, deprecation or `xml` the merge drops → the losing declaration's node verbatim Unmodeled
  (`degraded_lowering`) under `openapi:conflicting-redeclaration<pointer>` per §4.8, keyed by the
  losing declaration's pointer, with the `openapi/conflicting-redeclaration` diagnostic only where
  the two are unsatisfiable, an info `degraded-construct` naming a detail held differently, and
  nullability intersecting rather than dropping where the targets agree
- a boolean `false` `allOf` branch → the composed Model closed, branch verbatim Unmodeled
  (`degraded_lowering`) per §4.8, a `true` branch a silent no-op
- a shape applicator
  (`properties`/`patternProperties`/`additionalProperties`/`required`/`items`/`prefixItems`, and a
  `format` other than `password` where no type is declared) the lowered node has no field for →
  verbatim Unmodeled (`degraded_lowering`) per §4.8
- a parameter schema's `xml` and its `readOnly`/`writeOnly` → Parameter.Unmodeled (`no_ir_home`:
  Parameter has no field for either)
- `patternProperties` → AdditionalProps.Patterns
- `prefixItems` → Tuple.Elems and the `items` beside it → Tuple.Rest (`items: false` → none, an
  absent `items` → the top type, as 2020-12 reads it `true`), with
  `minItems`/`maxItems`/`uniqueItems` → Tuple.Constraints
- `x-*` → namespaced Unmodeled (legal on every object — hence Unmodeled on every node), read at
  every object that admits one: an object lowering to a node with a map of its own keeps them
  unscoped there, and one lowering to no node of its own is keyed by the path from its carrier down
  to it — on the document, `openapi:info/x-*`, `openapi:info/contact/x-*`,
  `openapi:info/license/x-*`, `openapi:externalDocs/x-*`, `openapi:components/x-*`,
  `openapi:tags/<i>/x-*`, `openapi:tags/<i>/externalDocs/x-*`
- on the service, `openapi:paths/x-*`
- on each operation the path item's `openapi:pathItem/x-*` plus `openapi:responses/x-*` and
  `openapi:externalDocs/x-*`
- on the HTTP binding, `openapi:callbacks/<name>/x-*`
- on the content, `openapi:encoding/<part>/x-*` and `openapi:itemEncoding/x-*`, `<part>` and
  `<name>` alike escaped RFC 6901 style so a document-chosen name stays one segment (§12)
- on the schema's type, `openapi:xml/x-*`, `openapi:discriminator/x-*`, `openapi:externalDocs/x-*`
- on the scheme, `openapi:flows/x-*` — since several such objects reach one map and an unscoped key
  would leave the survivor to lowering order (§12)
- a Link Object's own ride inside the verbatim `links` entry rather than taking a key beside it
- `$ref`-adjacent sibling keywords (3.1) and ref-target annotations merge onto the referencing
  Property/Parameter with **use-site precedence**, applied uniformly (oagen's ad-hoc per-site
  patching is the counterexample), and at a position carrying no Property/Parameter — an
  `allOf`/`oneOf`/`anyOf` branch, `items`, a component — bind an alias hoisted at that position
  instead, per §4.3 — constraints excepted, since bounds conjoin rather than override: each position
  keeps the ones it declared and none is copied to a use site (§12.2)
- a oneOf/anyOf whose variants are all string consts normalizes to a closed `Enum` in a `pass/`
  normalization — not in the compiler — so per-variant `Docs` survive until the collapse is chosen
- mutually-exclusive parameter groups (`x-mutually-exclusive-parameter-groups`) stay as namespaced
  Unmodeled entries, and their documented *promotion* (no dedicated node needed) is a pass that
  synthesizes one logical `Parameter` typed by a `Union` of variant models, bound via
  `HTTPParamBinding.ParamPath` per field
- pagination only via injectable policy, marked Inferred
- a component entry of a section with no registry — responses, parameters, examples, requestBodies,
  headers, links, callbacks, pathItems — that no lowering followed a reference to is kept verbatim
  on the document under `openapi:components/<section>/<name>` (`no_ir_home`, no diagnostic) rather
  than dropped (§12), and 3.2's components/mediaTypes is kept whole, since no reference into it is
  followed yet (GitHub #856)
- `$self` → that source's `SourceInfo.SelfURI` as written, a relative one unresolved, and at every
  minor, with a 3.0 or 3.1 document writing one warned `later-version-construct` as for every
  construct a later minor added (§12)
