# Cross-Spec Capability Matrix

What each source specification format can express, and what the Morphic IR must therefore be
able to represent without loss. The IR is designed against the **union** of these capabilities,
not the intersection — a generator target may ignore a capability, but the IR must never drop one.
Erlang/OTP is included as a frontend target: its "spec" is module type information
(`-spec`/`-type` on behaviour callbacks) plus the gen_server/gen_statem/gen_event message
protocols.

Legend: ✅ native concept · ⚠ expressible indirectly · — absent

| Capability | OpenAPI 3.x | Swagger 2.0 | TypeSpec | Smithy 2.0 | GraphQL | AsyncAPI | Protobuf | Erlang/OTP |
|---|---|---|---|---|---|---|---|---|
| Named object types | ✅ components.schemas | ✅ definitions | ✅ model | ✅ structure | ✅ type/input | ✅ schemas | ✅ message | ✅ -record/-type |
| Inline/anonymous types | ✅ | ✅ | ✅ | — (all named) | ⚠ | ✅ | ⚠ nested | ✅ type exprs |
| Inheritance / base types | ⚠ allOf | ⚠ allOf | ✅ extends | ⚠ mixins | ⚠ interfaces (conformance, not inheritance) | ⚠ allOf | — | — |
| Mixins / spread | — | — | ✅ spread | ✅ mixins | — | ⚠ traits | — | — |
| Tagged unions | ⚠ oneOf+discriminator | — | ✅ discriminated union | ✅ union | ⚠ union+__typename · ✅ @oneOf inputs (draft) | ⚠ oneOf | ✅ oneof | ✅ tagged tuples |
| Untagged unions | ✅ oneOf/anyOf | — | ✅ union | — | — | ✅ oneOf | — | ✅ \| |
| Intersection | ✅ allOf | ✅ allOf | ⚠ & (model is) | — | — | ✅ allOf | — | — |
| Negation | ✅ not | — | — | — | — | ✅ not | — | — |
| Enums (string) | ✅ | ✅ | ✅ named members | ✅ enum | ✅ | ✅ | ⚠ | ⚠ atom unions |
| Enums (numeric, valued) | ✅ | ✅ | ✅ | ✅ intEnum | — | ✅ | ✅ | ⚠ int unions |
| Open enums (unknown values allowed) | ⚠ anyOf trick | — | ⚠ union w/ string | ✅ (enums are open by default) | — | ⚠ | ✅ open (proto3/editions) / closed (proto2, per-enum feature) | ⚠ atom() fallback |
| Custom scalars | ⚠ type+format | ⚠ | ✅ scalar extends | ⚠ traits | ✅ scalar | ⚠ | — | ✅ -type/-opaque |
| Wire encoding hints (@encode / format) | ✅ format | ✅ format | ✅ @encode | ✅ timestampFormat | — | ✅ | ✅ fixed/zigzag/packed/delimited | — (ETF fixed) |
| Field wire IDs (numeric tags) | — | — | — | — | — | — | ✅ field numbers | ⚠ tuple positions |
| Wire name ≠ model name | ✅ (property key) | ✅ | ✅ @encodedName | ✅ jsonName (incl. union members) | — | ✅ | ✅ json_name | — |
| Optionality vs nullability distinct | ✅ (3.1) | ⚠ | ✅ | ⚠ presence only (null via @sparse collections) | ✅ | ✅ | ⚠ presence (3-state: implicit/explicit/required) | ⚠ :=/=> + 'undefined' |
| Defaults | ✅ | ✅ | ✅ | ✅ | ✅ args + input fields | ✅ | ✅ proto2 | ⚠ record fields |
| Constraints (min/max/pattern…) | ✅ | ✅ | ✅ decorators | ✅ traits | ⚠ directives (convention only) | ✅ | ⚠ protovalidate | ⚠ ranges, bit sizes |
| readOnly/writeOnly / visibility | ✅ | ✅ readOnly | ✅ @visibility classes | — | ✅ input vs output types | ⚠ (JSON Schema readOnly) | — | — |
| Recursive types | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Maps / additionalProperties | ✅ | ✅ | ✅ Record | ✅ map | — | ✅ | ✅ map | ✅ :=/=> |
| Tuples | ✅ prefixItems (3.1) | — | ✅ | — | — | ✅ | — | ✅ native |
| Literal types | ✅ const | ⚠ single enum | ✅ | — | — | ✅ | — | ✅ atoms/ints |
| Operations grouped by service/interface | ✅ tags (3.2 parent/kind) | ⚠ tags | ✅ interface/namespace | ✅ service/resource | ✅ Query/Mutation/Subscription | ⚠ | ✅ service | ✅ module |
| Resource hierarchy (CRUDL) | — | — | ⚠ @autoRoute | ✅ resource (incl. put, instance vs collection ops) | — | — | — | — |
| HTTP binding (method/path/status) | ✅ | ✅ | ✅ @route/@get… | ✅ http traits | — | ⚠ ws binding | ⚠ transcoding | — |
| Param styles (explode, matrix…) | ✅ style/explode | ⚠ collectionFormat | ✅ | ✅ | — | — | — | — |
| Multiple content types per body | ✅ | ⚠ consumes | ✅ @header contentType | ⚠ | — | ✅ | — | — |
| Multipart/form encoding | ✅ encoding | ✅ formData | ✅ multipart | — | — | — | — | — |
| Per-status error types | ✅ responses | ✅ | ✅ @error models | ✅ errors list (client/server fault) | — | — | ⚠ status codes | ⚠ {error, R} variants |
| Streaming: server (SSE/chunk) | ✅ itemSchema/sequential media types (3.2) | — | ✅ streams | ✅ eventstream | ✅ subscription | ✅ | ✅ stream | ⚠ info streams |
| Streaming: client / bidi | — | — | ✅ client · ⚠ bidi | ✅ | — | ✅ | ✅ | ⚠ cast/info flows |
| Events / pub-sub channels | ✅ webhooks (3.1) | — | ✅ events/sse | — | ✅ subscriptions | ✅ channels | — | ✅ gen_event/info |
| Callbacks / request-reply | ✅ callbacks | — | — | — | — | ✅ reply (static + dynamic address) | — | ⚠ From-reply |
| Pagination (first-class) | ⚠ x-* / links | — | ✅ @list/@pageItems + prev/first/last links | ✅ paginated trait | ⚠ connections | — | ⚠ AIP-158 | — |
| Long-running operations | ⚠ x-* | — | ⚠ Azure.Core @pollingOperation | ⚠ smithy.waiters | — | — | ⚠ google.longrunning | ⚠ send_request |
| Idempotency | ⚠ verb semantics | ⚠ | — | ✅ idempotent/@idempotencyToken | — | — | ✅ idempotency_level | — |
| Auth schemes | ✅ securitySchemes | ✅ | ✅ @useAuth | ✅ auth traits | — | ✅ (wide: SASL/X509/userPassword; attaches to servers) | ⚠ | — |
| Per-op auth override (AND/OR) | ✅ security | ✅ | ✅ | ⚠ OR only, priority-ordered | — | ✅ | — | — |
| Servers / endpoints | ✅ servers+vars (3.2 named) | ✅ host | ✅ @server | ⚠ @endpoint hostPrefix only | — | ✅ named servers+protocols+security | — | ⚠ nodes/registry |
| Protocol bindings (kafka/amqp/…) | — | — | — | — | — | ✅ bindings | — | ✅ behaviours |
| Versioning (added/removed) | — | — | ✅ @added/@removed | ⚠ @since | — | — | — | — |
| Deprecation w/ message | ✅ deprecated | ✅ | ✅ #deprecated | ✅ @deprecated | ✅ @deprecated(reason) | ✅ | ✅ | ✅ -deprecated |
| Examples | ✅ | ✅ | ✅ @example/@opExample | ✅ trait (input/output/error scenarios) | — | ✅ (header+payload pairs) | — | — |
| Docs: summary + description | ✅ | ✅ | ✅ @doc/@summary | ✅ @documentation | ✅ description | ✅ | ✅ comments | ✅ -doc/EDoc |
| Vendor extensions / traits / directives | ✅ x-* | ✅ x-* | ✅ decorators | ✅ traits | ✅ directives (ordered, repeatable) | ✅ x-* | ✅ options | ⚠ module attributes |
| One-way (fire-and-forget) operations | — | — | — | — | — | ✅ send w/o reply | — | ✅ cast |
| Positional wire encoding (records as tuples) | ⚠ prefixItems | — | ⚠ tuples | — | — | ⚠ items array | — | ✅ records/tuples |
| Symbol/atom literal values | — | — | — | — | — | — | — | ✅ atoms |
| Unsolicited server-initiated messages | ⚠ webhooks | — | — | — | ⚠ subscriptions | ✅ channels | — | ✅ info |
| Multi-format payload schemas | — | — | — | — | — | ✅ schemaFormat (Avro/Protobuf/RAML) | — | — |
| Third-party field extensions / extension ranges | — | — | — | — | — | — | ✅ extend/extensions | — |
| Field arguments (parameterized fields) | — | — | — | — | ✅ | — | — | — |
| Client-selectable response shape | — | — | — | — | ✅ selection sets | — | — | — |

## Consequences for the IR

1. **Unions are the hardest cross-cutting concept.** The IR union node must carry: variant list,
   optional discriminator (property name + value→variant mapping), whether the union is *tagged on
   the wire* (protobuf oneof, Smithy union — the wire format itself encodes the variant) vs
   *untagged* (JSON oneOf — variant inferred by validation), and open vs closed semantics
   (anyOf ≈ open, oneOf ≈ exactly-one). Discrimination is not always property-based: Erlang
   tagged tuples (and JSON arrays with a `const` head) discriminate by *position*, so the
   discriminator needs a tuple-index form; GraphQL `@oneOf` and Smithy unions are *key-tagged*
   (variant wire name is the object key).
2. **Optionality ≠ nullability ≠ presence.** Four distinct states exist (required non-null,
   required nullable, optional non-null, optional nullable); the IR keeps `Required` on the
   property and `Nullable` on the type reference. Protobuf adds a third axis — the *presence
   discipline* (implicit/explicit/required) — which is not nullability (protobuf has no null)
   and lives on its own field.
3. **Visibility must be lifecycle-based**, not a readOnly boolean: TypeSpec and GraphQL both need
   per-usage (create/read/update/query) property filtering, and TypeSpec's "visible in *no*
   lifecycle" (`@invisible`) is a distinct state from "unrestricted". OpenAPI readOnly/writeOnly
   lowers into it.
4. **Operations are protocol-neutral cores + protocol bindings.** The same operation node serves
   HTTP (OpenAPI), RPC (protobuf/Smithy), messaging (AsyncAPI), and actor protocols (OTP) by
   attaching different bindings. Streaming direction (unary, client, server, bidi) and
   one-way-ness (fire-and-forget: OTP cast, AsyncAPI send-without-reply, Thrift oneway, JSON-RPC
   notifications) live on the core, not the binding. One operation may carry *several* bindings
   of the same protocol (gRPC transcoding `additional_bindings`).
5. **Field identity is three names + an optional wire ID**: source name, IR canonical name,
   wire/serialized name, and numeric tag. The wire ID must admit zero (Cap'n Proto/FlatBuffers/
   Avro ordinals start at 0), and union *variants* need wire names too (Smithy `@jsonName` on
   union members). Generators derive language names; the IR never stores camelCase/PascalCase
   variants.
6. **Extensions are preserved, namespaced by origin** (`openapi:x-foo`, `smithy:aws.api#arn`,
   `graphql:@key`), so no source metadata is lost and later generators can opt into them. The
   escape hatch only holds if *every* node that can carry source metadata has an extensions
   slot — new spec revisions land fields on exactly the objects one forgets (responses, examples,
   security schemes, server variables).
7. **GraphQL's parameterized fields and selection sets** don't map to fixed operations; the IR
   models schema entry-points as operations with arguments, and keeps the full type graph so a
   generator can offer query-building. This is deliberately lossy for arbitrary client queries —
   acceptable because Morphic generates SDK surface, not persisted queries.
8. **Messages need identity.** AsyncAPI reuses one named message across channels, operations,
   and replies — messages live in a flat ID-keyed registry like every other named entity, and
   request-reply carries both a static reply channel and a *dynamic* reply address (a path into
   the request message's headers).
9. **Symbols are not strings.** Erlang atoms are a distinct term class (`ok` ≠ `<<"ok">>` on the
   wire); the Values channel carries a symbol kind so backends degrade to strings explicitly,
   never accidentally.
10. **Formats beyond this matrix already shape the IR.** Thrift/WSDL/Cap'n Proto service
    inheritance (services carry IDs and an extends list), Avro/XSD/OData decimal
    precision+scale, Avro aliases and enum fallback members, JSON-RPC positional params, and
    per-type namespaces (proto packages, Avro fullnames) are all held by the IR so those
    frontends never force a schema change.
