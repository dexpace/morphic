# Cross-Spec Capability Matrix

What each source specification format can express, and what the Morphic IR must therefore be
able to represent without loss. The IR is designed against the **union** of these capabilities,
not the intersection — a generator target may ignore a capability, but the IR must never drop one.

Legend: ✅ native concept · ⚠ expressible indirectly · — absent

| Capability | OpenAPI 3.x | Swagger 2.0 | TypeSpec | Smithy 2.0 | GraphQL | AsyncAPI | Protobuf |
|---|---|---|---|---|---|---|---|
| Named object types | ✅ components.schemas | ✅ definitions | ✅ model | ✅ structure | ✅ type/input | ✅ schemas | ✅ message |
| Inline/anonymous types | ✅ | ✅ | ✅ | — (all named) | ⚠ | ✅ | ⚠ nested |
| Inheritance / base types | ⚠ allOf | ⚠ allOf | ✅ extends | ⚠ mixins | ✅ interfaces | ⚠ allOf | — |
| Mixins / spread | — | — | ✅ spread | ✅ mixins | — | ⚠ traits | — |
| Tagged unions | ⚠ oneOf+discriminator | — | ✅ discriminated union | ✅ union | ⚠ union+__typename | ⚠ oneOf | ✅ oneof |
| Untagged unions | ✅ oneOf/anyOf | — | ✅ union | — | — | ✅ oneOf | — |
| Intersection | ✅ allOf | ✅ allOf | ⚠ & (model is) | — | — | ✅ allOf | — |
| Negation | ✅ not (3.1) | — | — | — | — | ✅ not | — |
| Enums (string) | ✅ | ✅ | ✅ named members | ✅ enum | ✅ | ✅ | ⚠ |
| Enums (numeric, valued) | ✅ | ✅ | ✅ | ✅ intEnum | — | ✅ | ✅ |
| Open enums (unknown values allowed) | ⚠ anyOf trick | — | ⚠ union w/ string | ✅ (enums are open by default) | — | ⚠ | ✅ (proto3 semantics) |
| Custom scalars | ⚠ type+format | ⚠ | ✅ scalar extends | ⚠ traits | ✅ scalar | ⚠ | — |
| Wire encoding hints (@encode / format) | ✅ format | ✅ format | ✅ @encode | ✅ timestampFormat | — | ✅ | ✅ fixed/zigzag |
| Field wire IDs (numeric tags) | — | — | — | — | — | — | ✅ field numbers |
| Wire name ≠ model name | ✅ (property key) | ✅ | ✅ @encodedName | ✅ jsonName | — | ✅ | ✅ json_name |
| Optionality vs nullability distinct | ✅ (3.1) | ⚠ | ✅ | ✅ | ✅ | ✅ | ⚠ presence |
| Defaults | ✅ | ✅ | ✅ | ✅ | ✅ args | ✅ | ✅ proto2 |
| Constraints (min/max/pattern…) | ✅ | ✅ | ✅ decorators | ✅ traits | ⚠ directives | ✅ | ⚠ protovalidate |
| readOnly/writeOnly / visibility | ✅ | ✅ readOnly | ✅ @visibility lifecycle | — | ✅ input vs output types | — | — |
| Recursive types | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Maps / additionalProperties | ✅ | ✅ | ✅ Record | ✅ map | — | ✅ | ✅ map |
| Tuples | ✅ prefixItems (3.1) | — | ✅ | — | — | ✅ | — |
| Literal types | ✅ const | ⚠ single enum | ✅ | — | — | ✅ | — |
| Operations grouped by service/interface | ⚠ tags | ⚠ tags | ✅ interface/namespace | ✅ service/resource | ✅ Query/Mutation/Subscription | ⚠ | ✅ service |
| Resource hierarchy (CRUDL) | — | — | ⚠ @autoRoute | ✅ resource | — | — | — |
| HTTP binding (method/path/status) | ✅ | ✅ | ✅ @route/@get… | ✅ http traits | — | ⚠ ws binding | ⚠ transcoding |
| Param styles (explode, matrix…) | ✅ style/explode | ⚠ collectionFormat | ✅ | ✅ | — | — | — |
| Multiple content types per body | ✅ | ⚠ consumes | ✅ @header contentType | ⚠ | — | ✅ | — |
| Multipart/form encoding | ✅ encoding | ✅ formData | ✅ multipart | — | — | — | — |
| Per-status error types | ✅ responses | ✅ | ✅ @error models | ✅ errors list | ⚠ errors union | — | ⚠ status codes |
| Streaming: server (SSE/chunk) | ⚠ text/event-stream | — | ✅ streams | ✅ eventstream | ✅ subscription | ✅ | ✅ stream |
| Streaming: client / bidi | — | — | ✅ | ✅ | — | ✅ | ✅ |
| Events / pub-sub channels | ✅ webhooks (3.1) | — | ⚠ | — | ✅ subscriptions | ✅ channels | — |
| Callbacks | ✅ callbacks | — | — | — | — | ⚠ reply | — |
| Pagination (first-class) | ⚠ x-* / links | — | ✅ @list/@pageItems | ✅ paginated trait | ⚠ connections | — | ⚠ AIP-158 |
| Long-running operations | ⚠ x-* | — | ✅ @pollingOperation | ⚠ | — | — | ✅ LRO (google) |
| Idempotency | ⚠ verb semantics | ⚠ | ⚠ | ✅ idempotent/@idempotencyToken | — | — | ✅ idempotency_level |
| Auth schemes | ✅ securitySchemes | ✅ | ✅ @useAuth | ✅ auth traits | ⚠ | ✅ | ⚠ |
| Per-op auth override (AND/OR) | ✅ security | ✅ | ✅ | ✅ | — | ✅ | — |
| Servers / endpoints | ✅ servers+vars | ✅ host | ✅ @server | ✅ endpoint | ⚠ | ✅ servers+protocols | — |
| Protocol bindings (kafka/amqp/…) | — | — | — | — | — | ✅ bindings | — |
| Versioning (added/removed) | — | — | ✅ @added/@removed | — | — | — | ✅ reserved |
| Deprecation w/ message | ✅ deprecated | ✅ | ✅ #deprecated | ✅ @deprecated | ✅ @deprecated(reason) | ✅ | ✅ |
| Examples | ✅ | ✅ | ⚠ | ✅ trait | — | ✅ | — |
| Docs: summary + description | ✅ | ✅ | ✅ @doc/@summary | ✅ @documentation | ✅ description | ✅ | ✅ comments |
| Vendor extensions / traits / directives | ✅ x-* | ✅ x-* | ✅ decorators | ✅ traits | ✅ directives | ✅ x-* | ✅ options |
| Field arguments (parameterized fields) | — | — | — | — | ✅ | — | — |
| Client-selectable response shape | — | — | — | — | ✅ selection sets | — | — |

## Consequences for the IR

1. **Unions are the hardest cross-cutting concept.** The IR union node must carry: variant list,
   optional discriminator (property name + value→variant mapping), whether the union is *tagged on
   the wire* (protobuf oneof, Smithy union — the wire format itself encodes the variant) vs
   *untagged* (JSON oneOf — variant inferred by validation), and open vs closed semantics
   (anyOf ≈ open, oneOf ≈ exactly-one).
2. **Optionality ≠ nullability.** Four distinct states exist (required non-null, required nullable,
   optional non-null, optional nullable); the IR keeps `Required` on the property and `Nullable`
   on the type reference.
3. **Visibility must be lifecycle-based**, not a readOnly boolean: TypeSpec and GraphQL both need
   per-usage (create/read/update/query) property filtering. OpenAPI readOnly/writeOnly lowers into it.
4. **Operations are protocol-neutral cores + protocol bindings.** The same operation node serves
   HTTP (OpenAPI), RPC (protobuf/Smithy), and messaging (AsyncAPI) by attaching different bindings.
   Streaming direction (unary, client, server, bidi) lives on the core, not the binding.
5. **Field identity is three names + an optional wire ID**: source name, IR canonical name,
   wire/serialized name, and numeric tag (protobuf). Generators derive language names; the IR never
   stores camelCase/PascalCase variants.
6. **Extensions are preserved, namespaced by origin** (`openapi:x-foo`, `smithy:aws.api#arn`,
   `graphql:@key`), so no source metadata is lost and later generators can opt into them.
7. **GraphQL's parameterized fields and selection sets** don't map to fixed operations; the IR
   models schema entry-points as operations with arguments, and keeps the full type graph so a
   generator can offer query-building. This is deliberately lossy for arbitrary client queries —
   acceptable because Morphic generates SDK surface, not persisted queries.
