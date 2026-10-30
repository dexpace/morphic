# Morphic IR design

The intermediate representation is the single contract between spec compilers and generator
emitters. These documents specify every node in the model and the semantics of the hard cases. The
pipeline around it is defined in [`architecture.md`](../architecture.md); the capability analysis it
is designed against is [`ir-spec-matrix.md`](../ir-spec-matrix.md); the evidence base is
[`prior-art.md`](../prior-art.md). How a source format lowers into the IR is stated under
[`docs/compilers/`](../compilers/openapi.md), a format at a time.

Type sketches are written as Go because the implementation will be Go, but these documents are the
spec — field names and shapes in them are normative, receiver methods and helpers are not.

## Where each part lives

**The document and its identity**

| Document | Owns |
|---|---|
| [document](./document.md) | `Document`: the root, its registries and the API's own metadata |
| [ir-version](./ir-version.md) | `IRVersion`: what moves it, what a consumer does on a mismatch, and how a bump lands |
| [version-history](./version-history.md) | What each generation changed, and what a consumer of the one before it sees |
| [identity](./identity.md) | IDs: the grammar, each class of ID, and what `irverify` holds them to |
| [naming](./naming.md) | `Naming`: source name, neutral word sequence, hint and aliases |
| [provenance](./provenance.md) | `Provenance` and `Diagnostic`: where a node or a finding came from |

**Types**

| Document | Owns |
|---|---|
| [type-graph](./type-graph.md) | `TypeDef` kinds, `TypeCommon` and `TypeRef` |
| [primitives](./primitives.md) | `Primitive`, `PrimKind` and containment between kinds |
| [scalars](./scalars.md) | `Scalar`, and the alias a position hoists over a shared target |
| [models](./models.md) | `Model`, `Discriminator`, composition and the flattening |
| [unions](./unions.md) | `Union`, `Variant` and tag modes |
| [enums](./enums.md) | `Enum`, `EnumMember` and member identity |
| [containers](./containers.md) | `List`, `MapT`, `Tuple`, `Literal`, `External`, `Any` |
| [properties](./properties.md) | `Property` and `Visibility` |
| [constraints](./constraints.md) | `Constraints`, `Encoding`, `XMLHints` |
| [values](./values.md) | `Value`, `BigVal` and the typed-data channel |
| [derived-facts](./derived-facts.md) | What `ir/irfacts` derives: subtypes, exposed properties, usage, recursion |

**The service layer**

| Document | Owns |
|---|---|
| [services](./services.md) | `Service`, `OperationGroup`, `ResourceInfo` |
| [operations](./operations.md) | `Operation`, `Parameter` |
| [payloads](./payloads.md) | `Payload`, `Content`, `PartEncoding`, `FileInfo` |
| [responses](./responses.md) | `Response`, `Header`, `ErrorCase`, and how a status is classed |
| [interaction-patterns](./interaction-patterns.md) | `Pagination`, `LongRunning`, streaming |
| [auth](./auth.md) | `AuthScheme`, `OAuthFlow`, `AuthRequirement` |
| [servers](./servers.md) | `Server`, the registry and the scopes that name it |
| [availability](./availability.md) | `Availability`, the API's version timeline |

**Protocol bindings**

| Document | Owns |
|---|---|
| [bindings](./bindings.md) | `OpBindings` |
| [http-binding](./http-binding.md) | `HTTPBinding`, `HTTPParamBinding`, webhooks |
| [rpc-binding](./rpc-binding.md) | `RPCBinding` |
| [messaging](./messaging.md) | `MessageBinding`, `Reply`, `Channel`, `Message` |
| [graphql-binding](./graphql-binding.md) | `GraphQLBinding` and GraphQL's lowering conventions |
| [otp-binding](./otp-binding.md) | `OTPBinding` and Erlang/OTP's lowering conventions |

**What stays outside the typed model**

| Document | Owns |
|---|---|
| [annotations](./annotations.md) | `Docs`, `Deprecation`, `Example`, and where a declaration of one lives |
| [unmodeled](./unmodeled.md) | `Unmodeled`, `RawConfig`, reasons, key scoping and promotion |
| [boundaries](./boundaries.md) | What the IR declines to model: validation-only keywords, degraded lowerings, exclusions |

[open-questions](./open-questions.md) records what is undecided. The lowering each format without a
compiler is planned to follow is [`planned-lowerings.md`](../compilers/planned-lowerings.md).

## Design rules

1. **Union of capabilities.** The IR is designed against everything any supported spec can express
   (`ir-spec-matrix.md`). An emitter may ignore a capability; a compiler must never drop one, so a
   construct the IR has no place for yet is kept verbatim under `no_ir_home`
   ([unmodeled](./unmodeled.md)). Only compilers are staged over time, and the surface they are
   designed against is a goal rather than a finished state: a later compiler is meant to fill fields
   rather than add them, and where one meets a gap, closing it is a change of the schema, versioned
   as [ir-version](./ir-version.md) says (architecture.md §2.5).
2. **Un-lowered.** Composition, unions, visibility, discriminators, encodings, and streaming stay
   in source-semantic form. Flattening and language fitting happen in emitters.
3. **Stable IDs, flat registries.** All named entities live in flat, ID-keyed registries and
   reference each other by ID. No node embeds another named node. Recursive types need no special
   handling; renames never break references.
4. **Three-plus-one naming.** Every nameable carries: the original source name, an IR canonical
   name, a wire name, and (where the format has one) a numeric wire ID. Language casing is never
   stored.
5. **Serializable.** The whole document round-trips through JSON deterministically (maps are
   emitted in sorted-key order; slices preserve source order). In memory it reads as the tree its
   JSON encodes: a pointer, slice or map may be shared, and is then written once per place that
   holds it, but none may hold itself, which has no encoding. `irverify` reports one that does
   (`ir/cyclic-structure`).
6. **Provenance everywhere.** Every node records where it came from and whether it was declared
   or inferred.

## Go representation of closed sums

Go has no sum types; the IR uses sealed interfaces with unexported marker methods, one concrete
struct per kind, and a `Kind()` accessor for switch-dispatch:

```go
type TypeDef interface {
    typeDef()          // sealed
    Kind() TypeKind
    Common() *TypeCommon
}
```

JSON encoding of sums uses an adjacent `kind` tag (`{"kind": "model", ...}`). Exhaustiveness is
enforced by a `switch`-completeness test over `TypeKind` values (the oagen `assertNever` lesson,
adapted to Go). Nothing is generated: the test iterates a hand-written kind list, and a second test
holds that list to the kinds the `ir` sources declare.

## Citing these documents

Name a document by its path from the repository root, and a section by its heading's anchor:
`docs/ir/models.md#the-flattening`. A Markdown page links instead, as these do. A citation carries
no section number, so a section can move within its document without touching what cites it, and
`TestDocuments_LinksResolve` holds every citation to a document and a heading, so one that moves
further fails until its citations follow.

## Former section numbers

These documents were one page, and issues, pull requests and commit messages from that time cite it
by section number. §3, §5 and §7 held no text of their own, so a citation of one means its
subsections.

| Section | Now |
|---|---|
| §1 | [Design rules](#design-rules) |
| §2 | [document](./document.md) |
| §2.1 | [ir-version](./ir-version.md), [version-history](./version-history.md) |
| §3.1 | [identity](./identity.md) |
| §3.2 | [naming](./naming.md) |
| §3.3 | [type-graph](./type-graph.md#type-references) |
| §4 | [type-graph](./type-graph.md) |
| §4.1 | [primitives](./primitives.md) |
| §4.2 | [scalars](./scalars.md) |
| §4.3 | [models](./models.md) |
| §4.4 | [unions](./unions.md) |
| §4.5 | [enums](./enums.md) |
| §4.6 | [containers](./containers.md) |
| §4.7 | [boundaries](./boundaries.md#validation-only-constructs) |
| §4.8 | [boundaries](./boundaries.md#degraded-constructs) |
| §4.9 | [derived-facts](./derived-facts.md) |
| §4.10 | [annotations](./annotations.md#decorators) |
| §5.1 | [properties](./properties.md) |
| §5.2 | [properties](./properties.md#visibility) |
| §5.3 | [constraints](./constraints.md) |
| §5.4 | [constraints](./constraints.md#xml-hints) |
| §6 | [values](./values.md) |
| §7.1 | [services](./services.md) |
| §7.2 | [operations](./operations.md), [payloads](./payloads.md), [responses](./responses.md) |
| §7.3 | [interaction-patterns](./interaction-patterns.md) |
| §8 | [bindings](./bindings.md) |
| §8.1 | [http-binding](./http-binding.md) |
| §8.2 | [rpc-binding](./rpc-binding.md) |
| §8.3 | [messaging](./messaging.md) |
| §8.4 | [graphql-binding](./graphql-binding.md) |
| §8.5 | [otp-binding](./otp-binding.md) |
| §9 | [auth](./auth.md) |
| §10 | [servers](./servers.md) |
| §11 | [availability](./availability.md) |
| §12 | [annotations](./annotations.md), [unmodeled](./unmodeled.md) |
| §12.1 | [annotations](./annotations.md#one-structural-home-per-declaration) |
| §12.2 | [annotations](./annotations.md#what-a-reference-carries-down) |
| §13 | [provenance](./provenance.md) |
| §14 | [`planned-lowerings.md`](../compilers/planned-lowerings.md) |
| §15 | [boundaries](./boundaries.md#deliberate-exclusions) |
| §16 | [open-questions](./open-questions.md) |
