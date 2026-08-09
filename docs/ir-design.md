# Morphic IR — Model Design

The intermediate representation is the single contract between spec compilers and generator
emitters. This document specifies every node in the model, the semantics of the hard cases, and
how each source format lowers into it. The pipeline around it is defined in
[`architecture.md`](./architecture.md); the capability analysis it is designed against is
[`ir-spec-matrix.md`](./ir-spec-matrix.md); the evidence base is [`prior-art.md`](./prior-art.md).

Type sketches are written as Go because the implementation will be Go, but this document is the
spec — field names and shapes here are normative, receiver methods and helpers are not.

---

## 1. Design rules

1. **Union of capabilities.** The IR represents everything any supported spec can express
   (`ir-spec-matrix.md`). A emitter may ignore a capability; a compiler must never drop one.
   Only compilers are staged over time — the IR's capability surface is complete from day one,
   so shipping the OpenAPI 3.x compiler first never forces an IR schema change when TypeSpec,
   Smithy, GraphQL, AsyncAPI, Protobuf, or Erlang/OTP compilers land.
2. **Un-lowered.** Composition, unions, visibility, discriminators, encodings, and streaming stay
   in source-semantic form. Flattening and language fitting happen in emitters.
3. **Stable IDs, flat registries.** All named entities live in flat, ID-keyed registries and
   reference each other by ID. No node embeds another named node. Recursive types need no special
   handling; renames never break references.
4. **Three-plus-one naming.** Every nameable carries: the original source name, an IR canonical
   name, a wire name, and (where the format has one) a numeric wire ID. Language casing is never
   stored.
5. **Serializable.** The whole document round-trips through JSON deterministically (maps are
   emitted in sorted-key order; slices preserve source order).
6. **Provenance everywhere.** Every node records where it came from and whether it was declared
   or inferred.

### Go representation of closed sums

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
enforced by a generated `switch`-completeness test over `TypeKind` values (the oagen
`assertNever` lesson, adapted to Go).

---

## 2. Document root

```go
type Document struct {
    IRVersion   string                 // version of the IR schema itself, semver
    Name        string                 // API title
    Version     string                 // API version string (source-declared)
    Docs        Docs
    Contact     *Contact               // {Name, URL, Email} (OpenAPI/AsyncAPI info.contact)
    License     *License               // {Name, Identifier, URL} (OpenAPI/AsyncAPI info.license)
    TermsOfService string
    Services    []Service              // ≥1; multi-service documents are normal (TypeSpec, stitching)
    Types       map[TypeID]TypeDef     // the type registry — the only owner of TypeDefs
    Channels    map[ChannelID]Channel  // event/messaging layer (AsyncAPI, webhooks, subscriptions, OTP processes)
    Messages    map[MessageID]Message  // message registry — messages are reused across channels and
                                       // referenced by identity from operations and replies (AsyncAPI 3)
    Auth        map[AuthID]AuthScheme  // auth scheme registry
    Servers     []Server               // endpoint templates
    TagDefs     []TagDef               // tag metadata registry: {Name, Docs}; tag *membership* stays
                                       // []string on the tagged nodes (OpenAPI/AsyncAPI tag objects)
    Versions    []string               // ordered version labels when availability metadata is used
    Unmodeled   Unmodeled
    Diagnostics []Diagnostic           // accumulated by compiler + passes; not part of API meaning
    Sources     []SourceInfo           // input files: format, path, content hash
}

type Contact struct { Name, URL, Email string }
type License struct { Name, Identifier, URL string }
type TagDef  struct { Name string; Docs Docs }
```

A `Document` is self-contained: no node references anything outside it.

---

## 3. Identity, names, references

### 3.1 IDs

```go
type TypeID string      // e.g. "t/openapi/components/schemas/User" or "t/anon/paths/~1users/get/responses/200/content/application~1json"
type OpID   string      // operation identity, same construction
type ServiceID, ChannelID, MessageID, AuthID, PropID string
```

IDs are opaque to consumers but constructed deterministically by compilers from the source
pointer of the defining occurrence, so the same input always yields the same IDs (stable
snapshots, cachable, diffable across spec revisions). IDs are never derived from display names
and never rewritten by renames. The `dedup` pass may alias two structurally identical anonymous
types; aliases are recorded so both IDs stay resolvable.

The shape around the pointer is one grammar every compiler shares: a kind prefix (`t`, `op`, `p`,
`s`, `auth`), then the namespace, then the path. Only the path is the format's, because a JSON
Pointer, a GraphQL structural path and a protobuf fully-qualified name are different things and
nothing outside the format can compute one. A node a lowering *mints* rather than finds takes a
namespace of its own, so no pointer a reference can spell ever reaches it — the general form of the
rule §4.3 states for distributed unions.

Primitives are the one exception, and only because the rule's premise does not hold for them: a
primitive occupies no source position, so there is no path for a format to own. Its identity is
its `PrimKind`, and its ID is `t/prim/<kind>` — derived by `ir.PrimTypeID` rather than by any
compiler, `ir` being the only place that can compute it. Two documents lowered from different
formats must reach that same node for the same kind, or they disagree about the identity of the
same type. The `prim` namespace is reserved for exactly those nodes: anything else addressed there
either collides with the primitive of that kind or squats the name of the next one. `irverify`
holds both halves — `ir/prim-id-not-derived` and `ir/prim-space-reserved` — for every document,
whatever produced it.

Every named entity has an ID — including services (Thrift `service B extends A`, WSDL 2.0
interface extension, and Cap'n Proto interface inheritance all reference services by identity)
and messages (AsyncAPI reuses one named message across channels, operations, and replies).

This is the direct answer to oagen's name-keyed registry (silent collision merging, string-rewrite
ref fixing) and Kiota's name-keyed children (collision reconciliation logic), and adopts the
intent of TCGC's `crossLanguageDefinitionId`.

### 3.2 Names

```go
type Naming struct {
    Source    string   // exactly as written in the spec ("user_id", "$ref name", GraphQL field)
    Canonical string   // IR-normalized identifier in neutral form: lower_snake words, no casing opinions
    Hint      string   // context-derived suggestion where there is no source name to render: an
                       // anonymous type ("connection_domain"), or one named with the empty string
    Aliases   []string // alternate names for schema-resolution matching (Avro aliases). Versionless —
                       // rename *history* tied to version labels lives in Availability.RenamedFrom.
}
```

`Canonical` is a cleaned word sequence, not a cased identifier — emitters apply casing, acronym
policy, and reserved-word escaping. Anonymous (hoisted) types have empty `Source` and a `Hint`;
whether a emitter inlines them or names them is its choice.

The segmentation is part of the contract, not each compiler's dialect: a word is a run of letters
and digits (with the combining marks that belong to them), a camel-case or letter/digit boundary
starts a new one, and **every other character separates** — `.`, `/`, `[`, `-`, a space alike. So
`com.example.User` and `com-example-user` canonicalize the same, and an emitter reading `Canonical`
never has to ask which compiler produced it. A source name with no word character in it
canonicalizes to the empty string; `Source` keeps the spelling.

At least one of `Source`, `Canonical` and `Hint` is set. A `Naming` with all three empty names the
entity to nobody, and every rule above is vacuously true of it, so `irverify` reports it
(`ir/naming-absent`). Where a source *does* name an entity with the empty string — a legal enum
value, property key, tag or component name — the compiler mints a hint rather than passing the
emptiness through; nothing is lost, because there was no spelling to keep.

**A camel-case boundary is where lowercasing changes the rune**, not where Unicode reports an
uppercase category. The two differ: double-struck `ℤ`, GREEK UPSILON WITH HOOK `ϒ` and the Roman
numerals are uppercase with no lowercase form, so they survive lowercasing unchanged and are not
boundaries — `COUNTℤ` is one word. The titlecase letters are boundaries and are not uppercase —
`xǅy` is two. Defining it by the effect rather than the category is what makes the grammar a fixed
point: a rune it split on but lowercasing left alone would still look like a boundary in the output,
and re-canonicalizing would split it again (GitHub #187). It is also the definition `irverify`
already applied to a whole canonical, so the producer and the checker agree by construction.

The **acronym tail** — the last capital of a run, before a lowercase letter — asks a different
question, and takes the category rather than the effect: `ℤServer` is `ℤ_server` because `ℤ` belongs
to a run of capitals whether or not lowercasing would change it, and `ǅBc` is `ǆ_bc` because a
titlecase letter belongs to one too. A transition test there would lose the first; the uppercase
category alone would lose the second.

`irverify` holds every `Naming` to this: it recomputes the canonical from the `Source` beside it, so
a boundary in the wrong place is a compiler bug rather than a variant reading. What no check can say
is that the grammar itself is right — a check that recomputes moves with what it recomputes through
— so the answers are pinned by a conformance table and the properties every answer must satisfy by
a fuzz target beside it (GitHub #186).

### 3.3 Type references

```go
type TypeRef struct {
    Target   TypeID
    Nullable bool     // this *usage* admits null on the wire
}
```

Nullability lives on the reference, not the target type, because the same type is nullable in one
position and not another. Combined with `Property.Required` this yields the four distinct states
(required/optional × nullable/non-null) that OpenAPI 3.1, TypeSpec, and GraphQL all distinguish.
Compilers normalize every source spelling to this one bit: OAS 3.0 `nullable: true`, OAS 3.1
`type: [T, "null"]`, TypeSpec `T | null`, GraphQL's absence-of-`!`.
A oneOf/anyOf/union whose only distinction is a null variant becomes a plain nullable `TypeRef`,
never a union node.

Protobuf field *presence* is **not** nullability — protobuf has no null. Presence disciplines
lower to `Property.Presence` (§5.1), keeping `Nullable` strictly about wire-null.

---

## 4. The type graph

```go
type TypeKind string // "primitive" | "scalar" | "model" | "union" | "enum" | "list" | "map" |
                     // "tuple" | "literal" | "external" | "any"

type TypeCommon struct {
    ID         TypeID
    Name       Naming
    Namespace  []string     // the type's declared logical namespace (proto package, Avro namespace,
                            // Thrift/XSD/Cap'n Proto scopes); independent of Service.Namespace
    Anonymous  bool         // hoisted inline type
    Docs       Docs
    Tags       []string     // free-form labels (Smithy @tags, OpenAPI tag membership on types via policy);
                            // tag metadata lives once in Document.TagDefs
    Sensitive  bool         // whole-type redaction (Smithy @sensitive on shapes); Property.Secret is the per-use form
    Access     string       // "" = public; "internal" = not part of the exported SDK surface
                            // (protobuf editions export/local, TCGC @access(internal), Smithy @internal via policy)
    Deprecation *Deprecation
    Availability *Availability
    Usage      UsageFlags   // computed by a pass: Input | Output | Error | Multipart | …
    WireNameByFormat map[string]string // type-level serialized-name overrides per mime
                                       // (TypeSpec @encodedName on models/enums/scalars)
    MediaTypeHint string    // declared default content type when the type is a body
                            // (TypeSpec @mediaTypeHint, Smithy @mediaType on string/blob shapes)
    XML        *XMLHints    // type-level XML wire shape: root element name/namespace
                            // (OpenAPI xml object, Smithy @xmlName/@xmlNamespace on shapes, TypeSpec @Xml.*)
    Examples   []Example    // typed example values attached to the type
                            // (TypeSpec @example, OpenAPI schema-level examples)
    Instantiation *TemplateInstantiation // provenance for monomorphized generics (TypeSpec templates)
    Unmodeled  Unmodeled
    Provenance Provenance
}

type TemplateInstantiation struct {
    Template string
    Args     []TemplateArg  // instances are identified by type AND value arguments
}
type TemplateArg struct {
    Type  *TypeRef          // exactly one of Type/Value is set
    Value *Value            // TypeSpec `valueof` template parameters
}
```

### 4.1 Primitives

```go
type Primitive struct {
    TypeCommon
    Prim PrimKind
}

type PrimKind string
// bool string bytes
// int8 int16 int32 int64 uint8 uint16 uint32 uint64
// integer  (arbitrary precision — JSON Schema "integer", TypeSpec "integer")
// float32 float64
// float    (arbitrary precision *binary* floating point — TypeSpec "float", supertype of float32/64)
// number   (arbitrary precision — JSON Schema "number", TypeSpec "numeric")
// decimal decimal128
// date time datetime datetime_offset duration
// url uuid
// any      (unknown/JSON any — schemaless)
```

The set is the union of TypeSpec's intrinsic scalars, JSON Schema's types, and Protobuf's needs.
Protobuf's `fixed32`/`sfixed64`/`sint*` are **encodings of** `uint32`/`int64`/…, not distinct
primitives — they lower to `Encoding` (§5.3). There is no `void` type: absence of a body/return
is a `nil *TypeRef`. TypeSpec `safeint` is not a primitive either — its canonical lowering is
`Scalar{Base: integer, Constraints: ±(2^53−1)}`.

### 4.2 Scalars (named restricted/extended primitives)

```go
type Scalar struct {
    TypeCommon
    Base        *TypeRef     // primitive or another scalar — extension chains preserved.
                             // nil = opaque scalar with implementation-defined representation
                             // (GraphQL custom scalars declare no base; emitters map nil-base
                             // scalars to their opaque-scalar strategy, not a fabricated chain)
    Constraints *Constraints
    Encoding    *Encoding
}
```

Covers TypeSpec `scalar X extends Y`, GraphQL custom scalars, OpenAPI `type+format` pairs that a
compiler chooses to name, Smithy simple shapes with traits. Emitters resolve the chain to the
nearest representable base and accumulate constraints/encoding along the way.

`Base` is also the edge an *alias* uses, and there it names a node of any kind: §14's rule gives a
position that declares something a node of its own, and where that position resolved to a shared
target — a `$ref` to a model, a union, an enum — the node it owns is a `Scalar` standing over it.
So a consumer following `Base` must expect a non-scalar and stop there rather than assume a
restriction chain; `Constraints` and annotations on such a node bind the position, not the target.
`pass/validate` reads composition and discriminator subtyping through exactly this edge.

### 4.3 Models (structs / objects / messages)

```go
type Model struct {
    TypeCommon
    Properties     []Property
    Base           *TypeRef        // declared single inheritance (TypeSpec extends, allOf-as-inheritance)
    Implements     []TypeRef       // interface conformance, N-ary (GraphQL `implements A & B`); targets are Abstract models
    Mixins         []TypeRef       // composition without subtyping (Smithy mixins, TypeSpec spread provenance, extra allOf entries)
    AdditionalProps *AdditionalProps // map-like catch-all alongside declared properties
    Additional     AdditionalMode  // openness of the property set beyond declared + AdditionalProps
    Constraints    *Constraints    // cardinality of the property set (minProperties/maxProperties);
                                   // Additional says whether undeclared properties are allowed at all,
                                   // this says how many an instance may carry
    Abstract       bool            // cannot be instantiated directly; fields may be typed by it and
                                   // resolve to a conforming concrete model (GraphQL interface types)
    Positional     bool            // properties serialize positionally as a tuple ordered by WireID
                                   // (Erlang records: tag at element 1, fields at 2..N; WireID is the
                                   // 1-based element index, matching element/2)
    ExtensionRanges []WireIDRange  // wire-ID ranges reserved for third-party extension fields
                                   // (protobuf `extensions 100 to 199`)
    Discriminator  *Discriminator  // set on the polymorphic base
    DiscriminatorValue string      // set on each subtype: its wire tag value
    InputOnly      bool            // GraphQL input types; distinct identity from output types
}

type AdditionalMode string
// ""                         — unspecified (open by JSON Schema default)
// "closed"                   — no properties beyond the declared set (additionalProperties: false,
//                              closed-by-construction records)
// "closed_after_composition" — closed once composition is resolved (unevaluatedProperties: false)

type WireIDRange struct { From, To int }

type AdditionalProps struct {
    Value    TypeRef
    Key      *TypeRef       // nil = string keys
    Patterns []PatternProps // key-pattern-scoped value schemas (JSON Schema patternProperties)
}
type PatternProps struct { Pattern string; Value TypeRef }

type Discriminator struct {
    // Exactly one of Property / PropertyName / Index locates the tag:
    Property PropID                // model hierarchies: the property that carries the tag
    PropertyName string            // unions: wire name of the tag property (it exists on no single
                                   // model, so there is no PropID to point at; TypeSpec @discriminated
                                   // discriminatorPropertyName, OpenAPI discriminator on oneOf)
    Index    *int                  // positional: 0-based tuple element carrying the tag Literal
                                   // (Erlang tagged tuples {ok, V} | {error, R}; JSON arrays with a
                                   // const head via prefixItems)
    Mapping  map[string]TypeID     // wire value → subtype; nil mapping = infer by type name
    Default  TypeID                // variant to use when the tag is absent/unrecognized
                                   // (OpenAPI 3.2 defaultMapping); zero = none
    Envelope string                // "" = tag inline in the variant object;
                                   // "object" = {kind, value} wrapper (TypeSpec @discriminated envelope)
    EnvelopeValueName string       // wire name of the envelope's value property (default "value";
                                   // TypeSpec envelopePropertyName); meaningful only when Envelope=="object"
    Inferred bool                  // discovered heuristically (const-property detection), not declared
}
```

**Composition semantics.** `Properties` contains only the model's *own* properties. Consumers
walk `Base`, `Implements`, and `Mixins` for the full shape (a provided `FlattenedProperties()`
traversal helper does this; the flattening is computed, never stored). This preserves what oagen
and Kiota lose: the difference between "inherits from", "conforms to", "mixes in", and
"declares". `Base` is single (subtype identity); `Implements` is N-ary conformance to `Abstract`
models — the distinction matters because GraphQL allows multiple interfaces while every
class-based target language allows at most one base class.

How compilers classify `allOf`: an entry that is a `$ref` to a schema participating in a
discriminator hierarchy, or the sole `$ref` entry, becomes `Base`; other `$ref` entries become
`Mixins`; inline entries merge into `Properties` (with provenance). This mirrors Kiota's
decision table but keeps every branch reconstructible.

A `$ref` entry that also writes keywords beside the `$ref` composes the *branch*, not the schema
the branch names: those keywords bind this use of the target and cannot go on the shared node, so
the branch position takes a node of its own — an alias over the target carrying them — and
`Base`/`Mixins` point at that. It is the same alias any other `$ref`-with-siblings position hoists
when it carries no `Property` or `Parameter` of its own to hold them — `items`, a union branch, a
component — so nothing about composition is special here; a `$ref` entry writing nothing beside
itself composes straight to the target and hoists no node. The alias's name hint comes from the `$ref`
target, which is also what an outside `$ref` naming the branch pointer derives, since either may
be the lowering that interns the node.

`Base` and `Mixins` name whatever the source composed with, which in JSON Schema is any schema:
`allOf: [{$ref: SomeScalar}]` is legal and lowers to a `Base` pointing at a `Scalar`, and the same
holds for an `Enum`, a `Union`, or the top type. They are typed `TypeRef` rather than "reference
to a `Model`" for exactly that reason — narrowing them would mean dropping a branch the source
wrote, against invariant 2. What is *shaped* like `allOf` is the composition itself: one referent
per conjunct, resolved by reference. Emitters that can only render model inheritance are the ones
that must lower a non-`Model` conjunct to something their target can express.

**Composition co-declared with a union.** JSON Schema conjoins sibling keywords, so
`{allOf: […], oneOf: [{$ref: A}, {$ref: B}]}` is a composition *and* a union at once. A compiler
must never return early on the combinator and drop the composition. It distributes instead —
`S ∧ (X | Y)` is lowered as `(S ∧ X) | (S ∧ Y)`: the node is a `Union` whose every `Variant.Type`
is a `Model` carrying `S`'s own `Base`/`Mixins`/`Properties`/`required`/`Additional`, with that
branch's referent conjoined by the same rule as an `allOf` entry (`Base` when the composition
named none, else a `Mixin`). The composition rides on every variant rather than being merged into
one or dropped, so nothing degrades and no `allOf` branch is flattened. A property `S` declares
appears on each variant under its single source-derived `PropID`, since the source declares it
once.

The variant models are anonymous and **synthesized**: no schema in the source occupies them, so
each takes an ID in a namespace of its own, derived from — but not equal to — the ID the branch
pointer would yield. Provenance still points at the branch, the nearest thing the source wrote.
Giving them the branch pointer's own ID would put two different types on one pointer: a `$ref` to
`…/oneOf/N` denotes the *branch schema*, and whichever of the two lowered first would take the
pointer from the other. That makes the IR depend on declaration order and can produce exactly the
`Union` the next paragraph forbids.

Distribution needs a referent per branch, because `Base`/`Mixins` conjoin by reference — a branch
must be a `$ref`, and that `$ref` must resolve. A union that cannot supply a referent for every
branch is not distributed *at all* — never halfway, which would leave a `Union` whose variants
disagree about whether they carry `S` with nothing in the IR saying which. Those cases degrade
instead, enumerated in §4.8.

### 4.4 Unions

```go
type Union struct {
    TypeCommon
    Variants      []Variant
    Exclusive     bool           // oneOf/tagged: exactly one variant matches; anyOf: one-or-more
    WireTagged    bool           // the wire format itself encodes the variant (protobuf oneof,
                                 // Smithy union, GraphQL __typename) vs untagged JSON oneOf
    Discriminator *Discriminator // internal tag property, when one exists
}

type Variant struct {
    Name       Naming    // named variants (TypeSpec, Smithy, protobuf oneof fields); Hint-only for bare oneOf members
    Type       TypeRef
    WireName   string    // serialized tag when it differs from Name.Source
                         // (Smithy @jsonName on union members, protobuf oneof json_name)
    WireID     *int      // protobuf oneof field number, Cap'n Proto/Avro ordinal; nil = none
                         // (pointer because 0 is a legal ordinal in several formats)
    XML        *XMLHints // @xmlName/@xmlNamespace on union members
    Event      *EventInfo // event-stream metadata when the union is a stream's event set
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Examples   []Example
    Unmodeled  Unmodeled
}

type EventInfo struct {
    ContentType string   // per-event content type (TypeSpec @Events.contentType)
    Terminal    bool     // receiving this event ends the stream (TypeSpec @SSE.terminalEvent)
}
```

One node covers the whole design space from the capability matrix: untagged `anyOf`
(`Exclusive=false`), untagged `oneOf` (`Exclusive=true`), discriminated `oneOf`
(`+Discriminator`), natively tagged unions (`WireTagged=true`). Variant identity survives so
emitters can generate named accessors, sealed interfaces, or wrapper types per their refiner
strategy. Unions never degrade to optional-field merges in the IR — that includes GraphQL
`@oneOf` input objects, which are spec-level tagged input unions and lower here, not to models.

**Tag-mode conventions** (normative): `WireTagged=true` with a `Discriminator` means the tag is
a property inside the variant payload (GraphQL `__typename`). `WireTagged=true` with **nil**
`Discriminator` means the union is *key-tagged*: the wire shape is a single-key object whose key
is the variant's wire name (Smithy unions, proto3-JSON oneof, GraphQL `@oneOf` inputs).
`WireTagged=false` means the variant is inferred by validation (JSON oneOf/anyOf).

### 4.5 Enums

```go
type Enum struct {
    TypeCommon
    ValueType PrimKind      // string | int32 | int64 | float64 …
    Members   []EnumMember
    Closed    bool          // false = open/extensible: unknown values must be representable
    Flags     bool          // bitfield semantics
    FallbackMember string   // wire name of the member to substitute when an unknown value is read
                            // (Avro enum `default` symbol); "" = none
}

type EnumMember struct {
    Name       Naming
    Value      Value        // typed value, matches ValueType
    WireName   string       // serialized form when it differs from Value (rare)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability // members appear/disappear across versions (TypeSpec @added on EnumMember)
    Examples   []Example
    Unmodeled  Unmodeled
}
```

`Closed` defaults per source semantics: JSON Schema enums are closed; Smithy enums are open;
protobuf enums are open or closed **per file syntax / editions feature** (proto2 closed, proto3
open, editions `enum_type` per enum) — the compiler lowers the *resolved* value. Emitters that
can't express open enums (plain Go consts, TS string literals) lower via their refiners — the
bit must survive to that point (Kiota's string-only closed enums are the counterexample).
Duplicate member values are legal (protobuf `allow_alias`); slice order preserves which name is
canonical for serialization, and the validate pass must not reject them.

### 4.6 Containers and the rest

```go
type List struct    { TypeCommon; Elem TypeRef; Constraints *Constraints; Encoding *Encoding }
                    // Constraints: minItems/maxItems/uniqueItems.
                    // Encoding: container-level wire encoding — protobuf packed vs expanded repeated
                    // fields ("packed"/"expanded"); stacks with the element's own encoding
                    // (repeated sint64 [packed=false] = zigzag element + expanded list)
type MapT struct    { TypeCommon; Key TypeRef; Value TypeRef }             // Record/additionalProperties-only/proto map
type Tuple struct   { TypeCommon; Elems []TypeRef }                        // prefixItems, TypeSpec tuples, Erlang tuples
type Literal struct { TypeCommon; Value Value }                            // const / single-value enum / discriminator pins
type External struct{ TypeCommon; Identity string; Package string; MinVersion string } // resolve to a well-known library type (TCGC external)
type Any struct     { TypeCommon }                                         // schemaless
```

Containers are real type nodes with IDs (hoisted like all anonymous types), not flags on
references — Kiota's `CollectionKind` flag couldn't express nested collections or constrained
lists cleanly. `External` also carries opaque runtime handles that no target can structurally
model (`erlang:pid`, `erlang:fun` — see §4.8).

### 4.7 Validation-only schema constructs — a documented boundary

JSON Schema's `not`, `if`/`then`/`else`, `dependentSchemas`, `dependentRequired`,
`contains`/`minContains`/`maxContains`, `propertyNames`, `unevaluatedItems`, and
`unevaluatedProperties` express *validation logic*, not data shape; no target language's type
system represents them, and none of the other source formats has an equivalent. The IR
deliberately does not model them structurally. Compilers keep them verbatim in `Unmodeled`
(`openapi:not`, `openapi:if-then-else`, `openapi:contains`, …) under `ReasonValidationOnly` and
emit an `info` diagnostic, so the information is never silently lost and a future
validation-oriented emitter (request validators, mock servers) can still consume them — selecting
on the reason rather than sifting vendor metadata out of the same bag (§12).
The one structural carve-back: `unevaluatedProperties: false` is *shape* (a closed model after
composition) and lowers to `Model.Additional = closed_after_composition`. Every other
`unevaluated*` form stays verbatim, `unevaluatedProperties` with a schema value included.
`propertyNames` is not a second carve-back onto `AdditionalProps.Key`: it
constrains *every* key of the object, including the ones `properties` declares — where there is no
`AdditionalProps` to hold a key type at all — and it is a whole schema rather than a type, so only
its degenerate `{type: string}` form names one.

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

Two neighbouring keyword groups are *not* part of this boundary, and are recorded here so their
treatment is stated rather than assumed:

- The content vocabulary is modelled, not preserved. `contentEncoding` lowers to `Encoding.Name`
  and `contentMediaType` to `Encoding.MediaType` (§5.3) on the scalar the position lowers to;
  where the position lowers to a shape with no `Encoding` field, both stay verbatim under
  `ReasonNoIRHome`. `contentSchema` is real data shape with no IR field at any position, so it is
  always verbatim under `ReasonNoIRHome` — a gap expected to close, not a boundary (§12).
- `$id`, `$schema` and `$vocabulary` identify and configure a JSON Schema *resource*. The IR
  identifies every type by a synthetic ID derived from its source pointer rather than by `$id`
  (§3), and describes one API surface rather than a resource graph, so it has no dialect axis and
  none is coming: they are kept verbatim under `ReasonOutOfScope`, and `$id` is explicitly *not*
  honoured as a base URI for reference resolution. `$comment` is dropped outright — 2020-12 §8.3
  forbids presenting it to end users, so no emitter may see it. `$dynamicAnchor` is consumed as a
  reference target rather than carried as content — it is what lets a `$dynamicRef` expand.
  `$anchor` is currently neither: Milestone 1 resolves same-document JSON pointers only, so a
  `$ref` naming an anchor is reported unresolved rather than resolving through it (GitHub #141).
  It is excluded here because the compiler does nothing with it today, not because dropping it is
  settled.

This is the one intentional carve-out from "lossless means structural": losslessness is
satisfied by verbatim preservation, and the carve-out is explicit rather than accidental. The
keyword lists above are held to the compiler rather than merely asserted. The OpenAPI compiler's
`TestVocabulary2020_12_EveryKeywordIsLoweredOrKept` enumerates the 2020-12 vocabularies keyword by
keyword and, for each, compiles three documents — the keyword absent, written with one value,
written with a second — requiring that the absent form differs from the first and the first from
the second. So it holds both the keyword's *presence* and its *value* to reaching the compiled IR,
by lowering or by verbatim preservation; a keyword that stops being carried fails there rather than
waiting for a reader to notice.

Two limits are worth stating, because the guarantee is a floor rather than an inventory. It
compares one pair of values per keyword, so a lowering that carried one spelling and dropped
another would still pass. And rows that legitimately change nothing assert the inverse and carry
a written justification — `$comment`, which the spec forbids surfacing; `$defs`, whose members
lower on demand through a `$ref`; `$dynamicAnchor`, read as a reference target; and `$anchor`,
which the compiler does not yet honour.

### 4.8 Degraded source constructs — documented lowerings

A few source constructs have no faithful target — either because the IR has no combinator for the
shape, or because no target language's type system holds it. Each gets a *normative* weaker
lowering, so the degradation is a decision rather than an accident, and — wherever that weaker
shape actually loses something — verbatim preservation under `ReasonDegradedLowering` beside it,
so the loss stays recoverable. A construct kept raw merely for want of an IR field at that
position is a different situation and carries `ReasonNoIRHome` instead (§12).

The list is the set of *source-construct* degradations: one entry per distinctive source shape the
IR declines to model as written. It is not a list of every lowering that can weaken a type. Two
whole-schema fallbacks sit outside it, because they answer to a compiler's own limits rather than
to anything about the construct: a schema nested past the compiler's lowering depth cap becomes
`any` with an `error` diagnostic, and a boolean `false` schema becomes a closed empty `Model` with
an `info` one.

The `false` rule holds wherever the schema appears, composition included. A `false` conjunct
(`allOf: [false, …]`) admits nothing, so the composition admits nothing: the composed `Model` is
closed and the branch is kept verbatim under `ReasonDegradedLowering` beside it, which is what
distinguishes it from a model that merely wrote `additionalProperties: false`. What the other
branches contributed stays — closing is the nearest shape the IR has, and emptying the model would
trade one silent loss for another. A `true` conjunct admits everything, so contributing nothing
from it is exact and nothing is reported.

- **JSON Schema `oneOf`/`anyOf` co-declared with structural keywords that cannot be
  distributed** (`{type: object, properties: {…}, oneOf: [{$ref: A}, {type: string}]}`) — the
  keywords conjoin, so the schema is an *intersection* of a structural type and a union.
  **The IR** has `Union` but no intersection combinator (§4.3's `Base`/`Mixins` composes one
  referent per conjunct, the shape `allOf` writes, and cannot state "conjoined with a union";
  §15 records why one was not added) — target languages are not the constraint here; TypeScript
  writes `{a: string} & (X | Y)` directly. Where the conjunction *distributes* it is not degraded
  at all: §4.3 gives the rule, and the result is an ordinary `Union` of composed `Model`s. What
  lands here is the residue — the five shapes distribution cannot reach:
  1. the structural body is not a `Model` (a `const`, an `enum`, a scalar, or a bare
     `required`/`additionalProperties`/`patternProperties` with neither `properties` nor a
     `type`), so there are no composition fields for the variants to carry;
  2. a branch is written inline rather than as a `$ref`, so it names no node for `Base`/`Mixins`
     to point at — a `type: null` branch included;
  3. a branch *is* a `$ref` but names no referent the compilation resolves — a cross-document
     target it does not intern, an undeclared component, a pointer addressing no schema. The
     branch is unusable for the same reason an inline one is, so it declines the same way rather
     than composing a variant whose branch quietly went missing; the broken reference is reported
     at `error` severity on top of the `info` below;
  4. a `discriminator` is declared, binding the branch schemas by name;
  5. `oneOf` and `anyOf` are both declared, so distributing one would drop the other.

  In all five, whatever structural body the schema declares is lowered — `properties` or a
  declared `type` yields a Model or typed node — and the union branches are kept verbatim in
  `Unmodeled["openapi:oneOf"]` / `["openapi:anyOf"]` + `info` diagnostic naming which of the five
  applied. Case 1's body-less sibling leaves nothing to build: it reduces to `any`, and the
  sibling itself is kept by the rule below rather than dropped, so the union's diagnostic and the
  sibling's are both reported. A `null` branch in this shape is **not** lifted to `Nullable`: the
  union it
  belongs to is no longer structural, so lifting one branch out of it would assert something the
  lowered type does not say. The degradation is that the generated SDK is *under-constrained* —
  it admits values the union would reject — which is why the branches must survive for a
  validating emitter to reach.

  A union whose branches declare no shape at all (`oneOf: [{required: [a]}, {required: [b]}]`)
  is not a degradation and is not listed here: it narrows the body without reshaping it, which
  is validation logic, so it is preserved under `ReasonValidationOnly` with §4.7's keyword family.
- **Competing keywords at one position, where only one can be the value** — keywords conjoin, so
  `{allOf: [{$ref: Base}], enum: [a, b]}` is a narrowing of `Base` rather than a malformed
  document and `{oneOf: […], anyOf: […]}` demands both, yet a position lowers to one node. Two
  families compete: the value keywords `const`, `enum` and `allOf`, elected in that order, and the
  two union combinators, where `oneOf` wins. **The IR** has no intersection combinator (§15, as
  above), so the position lowers as the elected keyword — the most it can express there — and each
  keyword the election passed over is kept verbatim in `Unmodeled["openapi:<keyword>"]` under
  `ReasonDegradedLowering` at that keyword's own pointer, plus one `openapi/degraded-construct`
  `info` naming what was elected and what was kept. A position reports only what it actually
  stored, so a payload that fails to convert is reported unpreservable rather than announced as
  kept. Two consequences are deliberate: the elected combinator still becomes a `Union` rather
  than degrading to the top type, because that `Union` is a shape the IR *can* express and
  discarding it as well would model nothing at all; and a `{X, null}` `oneOf` written beside an
  `anyOf` does **not** collapse to nullable `X` (§3.3), because the collapse asserts the position
  *is* nullable `X` while the co-declared `anyOf` conjoins with it, and collapsing would resolve
  the position to a shared primitive — which must never carry one declaration's keywords. Where a
  *structural* sibling is written as well, the bullet above applies instead and both branch sets
  are kept. A keyword the *elected* lowering never reads — `type: string` beside an `allOf`,
  `format` beside a `const` — is a separate question this document has not settled: `allOf` beside
  `type: object` is the common case and loses nothing, so it wants a per-winner rule rather than a
  keyword list.
- **An inline `allOf` branch declaring more than the merge consumes** — a branch written inline
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
  branch written `[*shared]` or `[{<<: *shared}]` derives the same residue as one written out. The diagnostic is a `warning` rather than an `info` where the branch declares a
  `type` that excludes `object`, because the composing `Model` then asserts a shape the source
  contradicts — `allOf: [{type: string, maxLength: 3}]` composes an empty model of kind `model`
  over a source that says `string`. Two branch kinds are outside this rule rather than limits of
  it: a `$ref` branch owns a node, so what is written beside the `$ref` is *modeled* on the branch
  alias §4.3 describes rather than kept as residue, and a boolean branch declares no keywords at
  all, so there is no residue to derive — it is a decision about the composed node instead, stated
  with the `false`-schema rule above.
- **A shape applicator the lowered node cannot carry** — `properties`, `patternProperties`,
  `additionalProperties`, `required`, `items` and `prefixItems` each constrain an instance whether
  or not a `type` is written beside them, and each has exactly one IR home: a `Model`'s property
  set, openness and pattern bindings, or a `List`'s element and a `Tuple`'s positions. A position
  whose lowering produced neither — an `Enum`, a `Literal`, a `Scalar`, or the top type — has
  nowhere to put them, and so does a position whose lowering produced the other of the two
  (`{type: object, properties: {…}, items: {…}}` lowers to a `Model`, which holds no element
  type). This covers both a contradictory schema, where one half is taken and the other must stay
  recoverable (`{type: string, enum: [a, b], properties: {f: …}}` is not satisfiable, so lowering
  to the enum is a reasonable degradation), and an applicator written with no type at all, where
  the position lowers to the top type — which says strictly less than the applicator does
  (`{items: {type: string}}` constrains every array instance). **The IR** could model the untyped
  case by reading the applicator as the shape it implies — an untyped `items` as a list — but
  that is a lowering decision this document has not taken, and keeping the source leaves it open.
  Each unhomed keyword is kept verbatim in
  `Unmodeled["openapi:<keyword>"]` under `ReasonDegradedLowering`, plus one
  `openapi/degraded-construct` `info` naming the kind the position lowered to and the keywords it
  cannot hold. Which keyword has a home is asked of the node the lowering produced, never
  re-derived from the dispatch that produced it. A position that reduced to a shared node gets an
  alias of its own first: a shared primitive must never carry one declaration's keywords. The
  `format` keyword joins the list only where no `type` is declared, since it is homed by pairing
  with a type rather than by a node kind. Two limits are deliberate: a multi-typed schema
  (`type: [string, object]`) is exempt, because it lowers one variant per declared type from the
  same schema and the homes are the variants'; and a `$ref` position is not covered, since its
  siblings bind the position rather than the referent and are handled at §4.3's use-site rule.
- **JSON Schema open tuples** (`prefixItems` with a trailing `items`) — a fixed positional head
  plus a homogeneous tail of unbounded length. **The IR** has `Tuple` (fixed arity) and `List`
  (homogeneous, one element type) and no node that is both; target languages are not the
  constraint (TypeScript writes `[string, number, ...boolean[]]`). The head lowers to a `Tuple`
  and the tail schema is kept verbatim in `Unmodeled["openapi:items-after-prefix"]` + `info`
  diagnostic. The degradation is that the generated SDK is *over-constrained* in the opposite
  direction to the union case above — the `Tuple` asserts an exact arity the source does not,
  so a longer instance no longer type-checks — which is why the tail must survive for an
  emitter that can express a rest element, or a validating emitter, to reach.
- **TypeSpec `never`-typed properties/variants** — no `never` node exists, so compilers delete
  the member and emit an `info` diagnostic. Nothing is preserved beside it: a `never` member
  admits no value, so there is no content the deletion could lose.
- **TypeSpec `StringTemplate` types** (interpolating non-literal types) — degrade to `string` +
  `Unmodeled["typespec:string-template"]` + `info` diagnostic.
- **Erlang bit-sized binaries** (`<<_:M, _:_*N>>`) — `bytes` + `Min/MaxLength` when
  byte-aligned, plus `Unmodeled["erlang:bit-size"] = {base, unit}` + `info` diagnostic.
- **Erlang `fun()` types** — `External{Identity: "erlang:fun"}` + full spec text in
  `Unmodeled["erlang:fun-spec"]` + `warning` diagnostic (non-portable member). No function
  type node exists.
- **Erlang heterogeneous map association lists** (`#{atom() => a(), integer() => b()}`) —
  lower to union-typed `AdditionalProps` + `Unmodeled["erlang:map-assocs"]` verbatim.
- **Erlang `-opaque`** — the shape lowers structurally and losslessly; what degrades is the
  abstraction boundary the attribute declares, which no IR node states.
  `Unmodeled["erlang:opaque"] = true` records it, and emitter policy decides whether to expose
  or wrap.

---

## 5. Properties, constraints, encodings, visibility

### 5.1 Property

```go
type Property struct {
    ID         PropID
    Name       Naming
    WireName   string              // serialized name; defaults to Name.Source
    WireNameByFormat map[string]string // per-mime overrides (TypeSpec @encodedName json/xml)
    WireID     *int                // protobuf field number / thrift id / tuple element index
                                   // (1-based when Model.Positional); nil = none — pointer because
                                   // 0 is a legal ordinal in Cap'n Proto/FlatBuffers/Avro
    ExtensionOf string             // "" = the model's own field; else the fully-qualified declaring
                                   // scope of a third-party extension field (protobuf `extend`) —
                                   // directs qualified JSON naming and registry-based accessors
    Type       TypeRef
    Required   bool                // presence on the wire; orthogonal to Type.Nullable
    Presence   PresenceKind        // wire-presence discipline where the format distinguishes more
                                   // than required/optional (protobuf)
    ClientOptional bool            // wire-required but clients MUST treat as optional
                                   // (Smithy @clientOptional; implicit for @input structure members)
    DefaultAdded bool              // default was added post-publication; generators may ignore it
                                   // for backward compatibility (Smithy @addedDefault)
    Visibility Visibility          // lifecycle set; zero value = visible in all
    Default    *Value
    Constraints *Constraints
    Encoding   *Encoding
    Args       []Parameter         // parameterized fields: GraphQL field arguments on any property,
                                   // at any depth — not just operation entry points. Empty elsewhere.
    Flatten    bool                // property's fields hoisted into parent on the wire (Smithy/TCGC
                                   // flatten; also set on the synthetic property wrapping a hoisted
                                   // protobuf oneof — oneof members are wire-level top-level fields)
    EventHeader bool               // in an event-stream event model: member travels in the frame
                                   // header, not the event payload (Smithy @eventHeader)
    EventPayload bool              // member is the *raw* frame payload — blob/string as bytes,
                                   // structure as protocol document (Smithy @eventPayload).
                                   // Mutually exclusive with EventHeader; at most one per model.
    Secret     bool                // redact in logs/docs (TypeSpec @secret, format:password)
    XML        *XMLHints           // XML wire shape when it diverges from the JSON-implied shape
    Examples   []Example           // property-level examples (TypeSpec @example, OpenAPI)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Unmodeled  Unmodeled
    Provenance Provenance
}

type PresenceKind string
// ""         — format default (JSON world: Required/Nullable say everything)
// "implicit" — absence ⇔ default value; unset is unobservable; zero values are not serialized
//              (proto3 no-label, editions IMPLICIT)
// "explicit" — unset is distinguishable from default-valued (hazzers/pointers; proto2 optional,
//              proto3 `optional`, editions EXPLICIT)
// "required" — must be present on the wire (proto2 required, editions LEGACY_REQUIRED)
```

**Scope rule for `Args`:** parameterized properties are only legal on models reachable from a
GraphQL binding (the only format with per-field arguments); the validate pass rejects them
elsewhere. This keeps ordinary models pure data shapes — a model without `Args` anywhere in its
graph serializes conventionally in every emitter.

### 5.2 Visibility — lifecycle sets, not booleans

```go
type Lifecycle = string // OPEN set. Canonical values: "create" | "read" | "update" | "delete" | "query".
                        // TypeSpec's visibility system allows arbitrary user-defined visibility
                        // classes; compilers lower custom classes as "<class>:<member>" strings so
                        // nothing is dropped. Filters evaluate per class, not across the flat union;
                        // emitters treat unknown classes as opaque filters.

type Visibility struct {
    Only []Lifecycle   // empty = visible in all lifecycles (unless None)
    None bool          // visible in NO lifecycle: excluded from every projection
                       // (TypeSpec @invisible) — distinct from the zero value
}
```

`readOnly` lowers to `Only: [read]` (plus delete/query per OpenAPI semantics); `writeOnly` to
`Only: [create, update]`; GraphQL input-vs-output types to `create/update` vs `read`; TypeSpec
`@visibility` maps directly. One logical model therefore produces N wire shapes; the projection
(`ModelShape(model, lifecycle)`, with PATCH additionally making properties optional *unless the
binding disables it* — §8.1 `PatchImplicitOptionality`) is a computed traversal in emitters'
plan layer — the IR stores the single logical model plus the visibility facts, never the
projected variants. Operations can override which filter applies to their request/response
(§7.2 `ParameterVisibility`/`ReturnTypeVisibility`). This is TypeSpec's `MetadataInfo` design
with storage and computation split.

### 5.3 Constraints and encodings

```go
type Constraints struct {
    // numeric — arbitrary-precision decimal strings, never float64 (TypeSpec Numeric lesson)
    Min, Max           *BigVal
    ExclusiveMin, ExclusiveMax bool
    MultipleOf         *BigVal
    Precision, Scale   *int64     // decimal digit bounds (Avro decimal, XSD totalDigits/fractionDigits,
                                  // OData Edm.Decimal)
    // string & bytes — length constraints apply to both (Avro fixed(N) = bytes, MinLength=MaxLength=N)
    MinLength, MaxLength *int64
    Pattern            string      // ECMA-262 regex as written; emitters translate or drop with a diagnostic
    PatternMessage     string      // human-readable validation message (TypeSpec @pattern's second arg)
    // collections
    MinItems, MaxItems *int64
    UniqueItems        bool
    MinProps, MaxProps *int64
}

type Encoding struct {
    Name     string    // "rfc3339" | "unixTimestamp" | "base64" | "base64url" | "iso8601" |
                       // "seconds" | "http-date" | "varint" | "zigzag" | "fixed" |
                       // "packed" | "expanded" | "delimited" | format strings
                       // (packed/expanded are container encodings on List.Encoding; "delimited" =
                       // protobuf group / editions DELIMITED message encoding)
    WireType *TypeRef  // the on-wire primitive when it differs from the logical type
                       // (utcDateTime encoded as int32; bytes as base64 string)
    MediaType string   // content media type of the value itself (Smithy @mediaType on string/blob,
                       // JSON Schema contentMediaType); "" = none
}
```

The logical-type / encoding-name / wire-type triple is TCGC's reification of TypeSpec `@encode`
and also absorbs OpenAPI `format` and Protobuf's `sint*/fixed*` wire variants. Encoding attaches
at the scalar definition or overrides at the property — property wins. Protobuf editions features
lower here per element after the compiler resolves the feature cascade (descriptors expose
resolved values): `field_presence` → `Presence`, `enum_type` → `Closed`,
`repeated_field_encoding` → `List.Encoding`, `message_encoding` → `"delimited"`; remaining axes
(`utf8_validation`, `json_format`) → namespaced `Unmodeled` entries.

### 5.4 XML hints

XML-capable formats (OpenAPI's `xml` object, Smithy's `xmlName`/`xmlAttribute`/`xmlNamespace`/
`xmlFlattened` traits, TypeSpec's `@Xml.*` decorators) describe an XML wire shape that diverges
from the JSON-implied one. This is typed, not an extension, because multiple formats express it
and emitters must act on it. Hints attach at both levels: `TypeCommon.XML` (root element
name/namespace of the shape itself) and `Property.XML` (per-use overrides; property wins).

```go
type XMLHints struct {
    Name      string   // element/attribute name override
    Namespace string   // namespace URI
    Prefix    string
    NodeType  string   // "" | "element" | "attribute" | "text" | "cdata" | "none"
                       // (OpenAPI 3.2 nodeType; "text" covers Smithy httpPayload text)
    Wrapped   bool     // list items wrapped in a container element
}
```

---

## 6. The Values channel

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "bytes" | "symbol" |
                          // "list" | "object" | "ref" | "ctor"
    Bool   bool
    Str    string         // payload for "string" AND "symbol"
    Num    BigVal         // arbitrary precision, decimal string form
    Bytes  []byte         // base64 in JSON form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
    Ref    *ValueRef      // reference to a declared constant: {Type TypeID; Member string}
                          // (TypeSpec enum-member defaults, references to named consts)
    Ctor   *CtorValue     // constructor-built value (TypeSpec scalar constructors)
}

type CtorValue struct {
    Scalar TypeID         // the scalar whose constructor is invoked
    Name   string         // constructor name ("fromISO", "now", custom inits)
    Args   []Value
}
```

`BigVal` is a decimal-string wrapper: no float64 round-tripping anywhere in the IR.

`symbol` is an interned-symbol value distinct from `string` (Erlang atoms: on the native wire
`ok` ≠ `<<"ok">>`). Emitters without a symbol concept render symbols as strings — explicitly,
not accidentally. `ctor` captures values built by named scalar constructors
(`utcDateTime.now()`, `plainDate.fromISO("2024-05-06")`) — inherently non-literal, so compilers
must not fold them.

---

## 7. Service layer

### 7.1 Services and operation groups

```go
type Service struct {
    ID         ServiceID
    Name       Naming
    Docs       Docs
    Version    string             // per-service version string (Smithy service version); the
                                  // document-level Version remains the API title's version
    Namespace  []string           // source namespace path (TypeSpec/Smithy/proto package)
    Extends    []ServiceID        // client-visible service inheritance (Thrift service extends,
                                  // WSDL 2.0 interface extension, Cap'n Proto interface inheritance);
                                  // inherited operations are walked, never copied
    Groups     []OperationGroup   // hierarchical; a group ≈ TypeSpec interface / Smithy resource / tag
    Auth       []AuthRequirement  // service-level default (OR-of-ANDs, §9)
    CommonErrors []ErrorCase      // errors every operation can return (Smithy service-level errors)
    Protocols  []ProtocolDecl     // declared serde/protocol conventions the service speaks
                                  // (Smithy @protocolDefinition traits like aws.protocols#restJson1)
    Renames    map[TypeID]Naming  // per-service shape presentation names (Smithy service `rename`);
                                  // the TypeID — and Naming on the type — are unchanged
    Servers    []int              // indexes into Document.Servers scoped to this service
    Unmodeled  Unmodeled
    Provenance Provenance
}

type ProtocolDecl struct {
    Name    string                // e.g. "aws.restJson1", "grpc"
    Options RawConfig             // per-protocol options kept raw (the Channel.Bindings pattern)
}

type OperationGroup struct {
    Name       Naming
    Docs       Docs
    Groups     []OperationGroup   // nesting: Smithy resources, sub-clients
    Operations []Operation
    Resource   *ResourceInfo      // Smithy resource semantics when declared
    Availability *Availability    // groups (TypeSpec interfaces) are versionable
    Unmodeled  Unmodeled
}

type ResourceInfo struct {
    Identifiers []Property                  // resource identity fields
    Properties  []Property                  // resource state fields (Smithy 2.0 resource properties)
    Lifecycle   map[string]OpID             // "create"|"put"|"read"|"update"|"delete"|"list" → op
                                            // (put = create-or-replace with client-provided identifier)
    NoReplace   bool                        // put may create but not replace (Smithy @noReplace)
    InstanceOps   []OpID                    // declared non-lifecycle instance operations (require identifiers)
    CollectionOps []OpID                    // declared collectionOperations — the split drives
                                            // sub-client shape and is a declared fact, not a heuristic
}
```

OpenAPI compilers build groups from tags (policy-controllable: tag-based vs path-prefix-based);
TypeSpec from interfaces/namespaces; Smithy from resources; GraphQL yields three groups
(query/mutation/subscription); Protobuf one group per `service`; Erlang/OTP one group per module.

### 7.2 Operation — the protocol-neutral core

```go
type Operation struct {
    ID         OpID
    Name       Naming
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability

    Params     []Parameter        // all logical inputs, protocol-unbound
    Request    *Payload           // body/message content, nil = none
    Responses  []Response         // ordered; success + alternative successes
    Errors     []ErrorCase        // declared failure shapes

    OneWay     bool               // fire-and-forget: no response EVER exists (OTP cast, AsyncAPI
                                  // send-without-reply, Thrift oneway, JSON-RPC notifications).
                                  // Distinct from a response with no body (an ack still exists);
                                  // validate rejects OneWay && len(Responses) > 0
    Streaming  StreamingMode      // none | client | server | bidi (derived summary of the two below)
    RequestStream  *StreamDetail  // client→server streaming semantics, when present
    ResponseStream *StreamDetail  // server→client streaming semantics, when present
    Pagination *Pagination
    LongRunning *LongRunning
    Idempotency Idempotency       // unknown | safe | idempotent | idempotency_token(param)
                                  // safe = no side effects (Smithy @readonly, HTTP GET semantics)
    Auth       []AuthRequirement  // override of service default; empty slice ≠ nil (empty = explicitly public)
    Tags       []string
    ParameterVisibility  []Lifecycle // visibility filter override for the request view
                                     // (TypeSpec @parameterVisibility); nil = protocol default
    ReturnTypeVisibility []Lifecycle // filter override for the response view; nil = protocol default
    OverloadOf *OpID              // TypeSpec @overload grouping

    Bindings   OpBindings         // §8 — how the core maps onto concrete protocols
    Examples   []Example
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Parameter struct {
    Name       Naming
    Type       TypeRef
    Required   bool
    Default    *Value
    Constraints *Constraints
    ValueFrom  *PropPath           // the parameter's value is derived from a location in the
                                   // outgoing/incoming message (AsyncAPI parameter `location`
                                   // runtime expressions) — SDKs may auto-fill it; nil = caller-supplied
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Examples   []Example
    Unmodeled  Unmodeled
    // NOTE: no location here — path/query/header is HTTP-binding detail (§8.1)
}

type Payload struct {
    Contents []Content            // one per media type / message schema — all kept
    Unmodeled  Unmodeled
}

type Content struct {
    MediaType string              // "application/json", "multipart/form-data", "" for non-HTTP
    SchemaFormat string           // schema *language* the type graph was lowered from, media-type
                                  // form (AsyncAPI multiFormatSchema: Avro/Protobuf/RAML/…);
                                  // "" = source-native. The verbatim source schema is preserved in
                                  // Unmodeled (e.g. "asyncapi:schema") for schema-registry workflows
    Type      TypeRef
    Item      *TypeRef            // element shape of a sequential stream declared per media type
                                  // (OpenAPI 3.2 itemSchema for SSE/JSONL/json-seq); nil = not sequential
    ItemEncoding *PartEncoding    // wire encoding of a sequential stream's item (3.2 itemEncoding);
                                  // singular like Item, because it governs every item alike.
                                  // Positional per-item encoding (3.2 prefixEncoding) has no form
                                  // here and stays in Unmodeled — see §12
    Encoding  map[PropID]PartEncoding // multipart/form: per-property (part) wire config, keyed by the
                                  // part property's PropID — a position inside the body model, so
                                  // `pass` resolves a key against that model rather than a registry
    File      *FileInfo           // body is a file upload/download (TypeSpec file bodies, binary payloads)
    Examples  []Example
    Unmodeled  Unmodeled
}

type PartEncoding struct {
    ContentTypes []string          // media type(s) of this part
    Headers      []Property        // per-part headers
    Multi        bool              // part repeats (array member → repeated parts)
    Filename     bool              // part carries a filename (file part)
    Style        string            // form-style serialization for non-file parts
    Explode      *bool
}
// TypeSpec tuple-form multipart lowers to a synthesized model whose properties are the parts.

type FileInfo struct {
    IsText   bool                 // textual vs binary contents
    Contents *TypeRef             // declared contents scalar chain (string/bytes extensions); nil = bytes
    ContentTypes []string         // declared *allowed* content-type set (TypeSpec File<"image/png" |
                                  // "image/jpeg">); runtime Content-Type comes from the file value
    ContentTypeDefault string     // default when the file value carries none
    FilenameLocation string       // "content-disposition" (default) | "path" | "header"
    FilenameWireName string       // wire name when FilenameLocation is path/header
}

type Response struct {
    Name       Naming             // for formats with named outputs; Hint elsewhere
    Conditions ResponseConditions // HTTP status codes/ranges; empty for RPC single-response
    Payload    *Payload           // nil = no body
    Headers    []Property         // response metadata fields
    StatusCodeProp *PropPath      // output member populated from the runtime HTTP status line
                                  // (Smithy @httpResponseCode, TypeSpec non-literal @statusCode);
                                  // the member is suppressed from the body
    Docs       Docs
    Unmodeled  Unmodeled
}

type ResponseConditions struct {
    StatusCodes []StatusRange     // {From,To}: 200–200, 400–499 ("4XX"), 0–0 = default/catch-all
}

type ErrorCase struct {
    Type       TypeRef            // an error-flagged model
    Conditions ResponseConditions
    Fault      string             // "" | "client" | "server" — protocol-neutral fault classification
                                  // (Smithy @error; OpenAPI 4XX/5XX is its HTTP lowering). Drives
                                  // exception hierarchies and default status synthesis
    Retryable  *bool              // Smithy @retryable; nil = unknown
    Throttling *bool              // retryable specifically due to throttling — distinct backoff class
                                  // (Smithy @retryable(throttling: true)); nil = unknown
    Docs       Docs
    Unmodeled  Unmodeled
}
```

Design notes:

- **Parameters carry no protocol location.** The same logical operation is bindable to HTTP
  (locations assigned in the binding), to gRPC (all params fold into a request message), or to a
  channel message. This is the split TCGC makes between `ServiceMethod` and `InputOperation`,
  turned inside out: Morphic stores the neutral core and each binding, and emitters derive the
  per-protocol view.
- **All responses and all content types survive** (oagen's "primary response" collapse is the
  counterexample). The plan layer picks a primary for SDK ergonomics; the IR doesn't.
- **Errors reference error models** (`Model` with `UsageFlags.Error`), with status conditions and
  range collapsing exactly as Kiota's error mappings (`404`, `4XX`, catch-all).
- **`OneWay` is on the neutral core**, like `Streaming` and `Idempotency`, because
  fire-and-forget is protocol-independent — an SDK that blocks awaiting a reply on a cast is
  wrong regardless of transport. The AsyncAPI compiler sets it for send-operations without a
  reply, rather than leaving one-way-ness inferable only from binding fields.

### 7.3 Pagination, long-running, streaming

```go
type Pagination struct {
    Strategy   PageStrategy   // cursor | offset | page | link_header | next_link | token
    Inferred   bool           // heuristic (OpenAPI name-matching policy) vs declared (Smithy/TypeSpec)
    InputCursor  *ParamPath   // which input continues iteration
    InputLimit   *ParamPath
    Items        *PropPath    // where result items live in the response — a PATH, not a name
    NextCursor   *PropPath    // continuation source in the response
    NextLink     *PropPath
    PrevLink     *PropPath    // server-driven navigation links beyond next
    FirstLink    *PropPath    // (TypeSpec @prevLink/@firstLink/@lastLink, OpenAPI links)
    LastLink     *PropPath
    TotalCount   *PropPath
}

type PropPath struct {
    Root     *TypeRef  // the type the path roots in; nil = determined by context
                       // (the enclosing response body, message payload, …)
    In       string    // "" = body/payload | "header" — continuation tokens and reply addresses
                       // can live in response/message headers, not just bodies
    Segments []PropID
}
type ParamPath struct{ Param string; Segments []PropID }

type LongRunning struct {
    FinalStateVia string       // "operation-location" | "status-monitor" | "original-uri" | …
    PollingOperation *OpID     // declared poll op (Azure.Core @pollingOperation — a library
                               // convention, not core TypeSpec)
    FinalOperation   *OpID     // declared final-result op (Azure.Core @finalOperation)
    PollingType   *TypeRef
    FinalType     *TypeRef
    ResultPath    *PropPath
}

type StreamingMode string      // "none" | "client" | "server" | "bidi"

type StreamDetail struct {
    Events  *TypeRef   // stream element type when it differs from the payload content type —
                       // for event streams this is a WireTagged Union of event models; within an
                       // event model, Property.EventHeader marks frame-header members and
                       // Property.EventPayload marks a raw-payload member (Smithy
                       // @eventHeader/@eventPayload); per-event content types and terminal
                       // events live on Variant.Event
    Initial *TypeRef   // initial-request / initial-response message preceding the stream
                       // (Smithy event stream initial messages); nil = none
    RequiresLength bool // the streamed content must have a known finite length up front
                        // (Smithy @requiresLength) — changes the generated parameter type
}
```

Streaming mode is on the operation core because it is protocol-independent (gRPC streams, SSE,
WebSocket, Smithy event streams all project onto it); the wire mechanism (SSE vs chunked vs
WS frames vs length-prefixed events) lives in the binding. Smithy **waiters**
(`smithy.waiters#waitable`) are *not* folded into `LongRunning` — acceptor lists and JMESPath
matchers are preserved verbatim in `Operation.Unmodeled` (§15).

---

## 8. Protocol bindings

```go
type OpBindings struct {
    HTTP    []HTTPBinding     // OpenAPI/Swagger ops, TypeSpec @route, Smithy http traits.
                              // A slice: one operation may carry several HTTP mappings —
                              // gRPC transcoding's additional_bindings — with the primary first
    RPC     *RPCBinding       // Protobuf/gRPC, Smithy RPC protocols, JSON-RPC
    Message *MessageBinding   // AsyncAPI operations, webhooks
    GraphQL *GraphQLBinding   // query/mutation/subscription field binding. GraphQL subscriptions
                              // bind here + streaming fields on the core — NOT via MessageBinding
                              // (GraphQL defines no channel; synthesizing one is deployment-aware
                              // policy, marked Inferred, never a compiler default)
    OTP     *OTPBinding       // Erlang/OTP behaviour operations (§8.5)
}
```

An operation has at least one binding; more than one is legal (Smithy service exposed over both
HTTP and RPC; gRPC with HTTP transcoding).

### 8.1 HTTP

```go
type HTTPBinding struct {
    Method      string             // as sent on the wire (OpenAPI 3.2 additionalOperations keys
                                   // carry exact capitalization; QUERY and custom methods are legal)
    URITemplate string             // RFC 6570 — the one true path representation (Kiota + TypeSpec agree)
    HostPrefix  string             // endpoint host prefix, may contain {param} labels (Smithy @endpoint)
    SharedRoute bool               // multiple operations legally share method+path, disambiguated by
                                   // request content (TypeSpec @sharedRoute) — validate must not
                                   // reject the duplicate, and single-route emitters must merge
    ParamBindings []HTTPParamBinding
    RequestContentTypes  []string  // priority-ordered
    ResponseBodyPath *PropPath     // HTTP response body = this sub-field of the response type
                                   // (gRPC transcoding response_body); nil = the whole payload
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    Compression *RequestCompression // client MUST compress the request body (Smithy @requestCompression)
    ChecksumRequired bool          // client MUST send a payload checksum (Smithy @httpChecksumRequired)
    PatchImplicitOptionality *bool // nil = protocol default (PATCH projections make properties
                                   // optional); false = disabled (TypeSpec @patch implicitOptionality)
    IsWebhook   bool               // OpenAPI 3.1 webhooks: direction is inbound
    Callbacks   []Callback         // out-of-band operations keyed by runtime expressions
    Unmodeled   Unmodeled
}

type RequestCompression struct { Encodings []string } // priority-ordered ("gzip", …)

type HTTPParamBinding struct {
    Param      string             // Operation.Params name it binds
    ParamPath  []PropID           // nested source field within the logical param, when the binding
                                  // targets a sub-field of a message-typed param (gRPC transcoding
                                  // {book.name}, dotted query params); empty = the whole param
    Location   HTTPLocation       // path | query | querystring | header | cookie | body |
                                  // body_property | host
                                  // host = param fills a HostPrefix label (Smithy @hostLabel)
                                  // querystring = the whole query string serialized from one schema
                                  //   (OpenAPI 3.2 in: querystring, combined with ContentType)
    WireName   string
    Style      string             // simple | form | label | matrix | deepObject | pipe/space-delimited
    Explode    *bool
    AllowReserved bool
    PathPattern string            // multi-segment path pattern constraint for this param
                                  // (gRPC transcoding {name=shelves/*/books/*}); "" = single segment.
                                  // The URI template uses reserved expansion; emitters that cannot
                                  // validate the pattern drop it with a diagnostic
    Prefix     string             // map-typed param spread as prefixed wire entries:
                                  // prefixed headers (Smithy @httpPrefixHeaders) or catch-all
                                  // query maps (@httpQueryParams with Prefix "")
    ContentType string            // param serialized as a media type (OpenAPI content-style params)
    BodyPath   []PropID           // for body_property: where in the body model it lands (TypeSpec HttpProperty)
}

type Callback struct { Expression string; Operations []OpID }
```

Every logical parameter is bound exactly once **per non-host location; a `host` binding is
additive** — Smithy `@hostLabel` members expand into the host prefix *and* still serialize at
their modeled location, by spec. TypeSpec's `HttpProperty` role+path flattening is the model
here: nested `@header`/`@body` annotations resolve to explicit `(role, wire name, path)`
bindings rather than restructured models.

### 8.2 RPC

```go
type RPCBinding struct {
    System      string     // "grpc" | "smithy-rpc" | "connect" | "jsonrpc" | …
    FullMethod  string     // "/pkg.Service/Method"
    InputType   *TypeRef   // the request message type params fold into (nil = synthesize from Params)
    ParamStructure string  // "" | "by_name" | "by_position" | "either" — how params serialize
                           // (JSON-RPC positional vs named; OpenRPC paramStructure). Param order
                           // is already source order per design rule 5; this is the mode
    IdempotencyLevel string
    Unmodeled   Unmodeled
}
```

### 8.3 Messaging / events

```go
type MessageBinding struct {
    Channel   ChannelID
    Direction MsgDirection     // send | receive (application perspective, AsyncAPI 3 semantics)
    Messages  []MessageID      // which of the channel's messages this operation uses
                               // (must be a subset — validated)
    Reply     *Reply           // request-reply semantics; nil = none. A send-op with no Reply and
                               // no Responses is one-way (set Operation.OneWay)
    Bindings  map[string]RawConfig // operation-level protocol bindings kept raw
                                    // (kafka groupId/clientId — constrain SDK client config)
    Unmodeled  Unmodeled
}

type Reply struct {
    Channel  *ChannelID    // static reply channel; nil when the address is dynamic-only.
                           // (An AsyncAPI reply channel's own address is null by spec)
    Address  *PropPath     // dynamic reply address: where in the *request* message the reply
                           // destination lives, e.g. In:"header", Segments:[replyTo]
                           // (AsyncAPI Operation Reply Address runtime expressions)
    Messages []MessageID   // reply payload message set (a subset of Channel's when set — validated)
    Docs     Docs
}

type Channel struct {
    ID        ChannelID
    Name      Naming
    Address   *string            // topic/routing key/path, may contain {params}.
                                 // nil = unknown/runtime-assigned address (load-bearing: reply
                                 // channels and dynamic topics; SDKs expose a runtime address arg)
    Docs      Docs
    Tags      []string
    Params    []Parameter
    Messages  []MessageID        // the channel's message set — messages live in Document.Messages
    Servers   []int
    Bindings  map[string]RawConfig // protocol-specific ("kafka", "amqp", "ws", "mqtt") kept as namespaced raw config
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Message struct {
    ID          MessageID
    Name        Naming
    Payload     Payload
    Headers     *TypeRef           // header *schema* — an object-constrained model hoisted into the
                                   // type registry like any anonymous type (headers can be named,
                                   // composed, even Avro-defined; and $message.header#/… paths need
                                   // a type to resolve against). Emitters compute flat lists per §4.3
    CorrelationID *PropPath        // In: "header" | "" (payload)
    ContentType string
    Tags        []string
    Docs        Docs
    Deprecation *Deprecation
    Examples    []Example          // correlated header+payload example pairs (Example.Headers + .Value)
    Bindings    map[string]RawConfig // message-level protocol bindings (kafka message key, …)
    Unmodeled   Unmodeled
    Provenance  Provenance
}
```

Kafka/AMQP/MQTT binding minutiae stay as namespaced `RawConfig` rather than modeled
structs — they are protocol deployment detail, not API shape, and modeling them structurally
would chase every AsyncAPI binding spec revision. The raw-bindings channel exists at every
level that AsyncAPI defines bindings: server, channel, operation, and message.

### 8.4 GraphQL

```go
type GraphQLBinding struct {
    Kind      string    // "query" | "mutation" | "subscription"
    FieldPath []string  // entry-point field (nesting for namespaced schemas)
    Unmodeled  Unmodeled
}
```

GraphQL lowering: schema types → models (`InputOnly` for inputs), interfaces → `Abstract`
models with implementors listing them in `Implements` (including interfaces implementing
interfaces), unions → `WireTagged` unions discriminated by `__typename`, **`@oneOf` input
objects → `Union{Exclusive: true, WireTagged: true}` with a variant per field** (never a model
of optional properties — that is the union-to-optional-fields collapse rule 2 forbids), custom
scalars → `Scalar` with `Base = nil`, entry-point fields → operations (field args → `Params`,
field type → response), nested field arguments → `Property.Args` at any depth, subscriptions →
`GraphQLBinding{Kind: "subscription"}` + `StreamingMode: server` + `ResponseStream`.

Directive conventions (normative): applications are ordered and repeatable, so
`Unmodeled["graphql:@<name>"]` holds an **ordered JSON array** of application argument objects
— a singleton array for a single application. Directive *definitions* are preserved verbatim at
document level under `Unmodeled["graphql:directive-definitions"]`. Type-extension assembly
(`extend type` across files): the node's `Provenance` points at the original definition, each
member's `Provenance` at its defining occurrence; the occurrence list goes to
`Unmodeled["graphql:extends"]` when SDL round-trip fidelity is wanted.

Arbitrary client-composed selection sets are out of scope by design: Morphic generates SDK
surface, and the full type graph — including per-field arguments — is retained so a emitter can
still offer query builders.

### 8.5 Erlang/OTP

```go
type OTPBinding struct {
    Behaviour  string    // "gen_server" | "gen_statem" | "gen_event"
    Kind       string    // "call" (synchronous request→reply) | "cast" (fire-and-forget; the
                         // operation also sets OneWay) | "info" (raw message send)
    Process    ChannelID // the channel modeling the target process: Address = registered name
                         // (nil Address = unregistered/runtime pid); registration kind
                         // (local/global/via) in Channel.Bindings["otp"]
    RequestTag *Value    // tag of the request tuple (a symbol Value, e.g. 'get');
                         // nil = the whole term is the request
    Unmodeled  Unmodeled  // source constructs the IR does not model, kept verbatim (§12)
}
```

An OTP compiler consumes module type information (`-spec`/`-type` on behaviour callbacks and
API functions). Lowering conventions: a gen_server ≈ one `Channel`; `handle_call`/`handle_cast`
APIs are `Operation`s with an `OTPBinding`; unsolicited `handle_info` messages are channel
`Messages` consumed via `MessageBinding{Direction: receive}`; a gen_event manager is a Channel
with one message shape per event. Tagged-tuple protocols (`{ok, V} | {error, R}`) are Unions of
Tuples discriminated by `Discriminator.Index`; records are `Model{Positional: true}` with the
record tag as an index-1 `Literal`-typed property. Splitting a tagged reply union into
`Responses` vs `Errors` (`{error, Reason}` is in-band data, not transport failure) is injectable
compiler policy, marked `Inferred`. Erlang `string()` is `[char()]` and must lower as a List of
a char-ranged scalar — never as PrimKind `string`. Delayed replies (`noreply` +
`gen_server:reply/2`) are invisible at the protocol surface and need no representation; call
timeouts and `multi_call`/`send_request` machinery are SDK runtime policy, not IR.

---

## 9. Auth

```go
type AuthScheme struct {
    ID   AuthID
    Name Naming
    Kind AuthKind
    // AuthKind: apiKey | http_basic | http_bearer | oauth2 | openid_connect | mutual_tls |
    //           user_password | x509 | symmetric_encryption | asymmetric_encryption |
    //           sasl_plain | sasl_scram_sha256 | sasl_scram_sha512 | sasl_gssapi | custom
    // The SASL family, user_password, and x509 are how Kafka/AMQP clients authenticate
    // (AsyncAPI securitySchemes). AsyncAPI `httpApiKey` lowers to apiKey; AsyncAPI `apiKey`
    // (transport user/password slot) lowers to apiKey with In: user|password. X509 is distinct
    // from mutual_tls (certificate as credential vs mutual verification) — compilers must not
    // conflate them.
    Docs Docs
    Deprecation *Deprecation
    // apiKey
    In       string      // header | query | cookie | user | password
    KeyName  string
    // http
    Scheme       string  // bearer, basic, digest…; also legal for apiKey (Smithy @httpApiKeyAuth scheme)
    BearerFormat string
    // oauth2
    Flows []OAuthFlow    // {Kind: authorization_code|client_credentials|implicit|password|device,
                         //  AuthorizationURL, TokenURL, RefreshURL, Scopes map[string]string,
                         //  Unmodeled} — device flow's deviceAuthorizationUrl rides AuthorizationURL
    OAuth2MetadataURL string // RFC 8414 authorization-server metadata (OpenAPI 3.2)
    // openid
    OpenIDConnectURL string
    Unmodeled  Unmodeled
    Provenance Provenance
}

type AuthRequirement struct {          // one *option*
    Schemes []SchemeUse                // ALL must be satisfied together
}
type SchemeUse struct { Scheme AuthID; Scopes []string }
// []AuthRequirement on a service/operation/server = OR across options (TypeSpec/OpenAPI security
// semantics). Option order is PRIORITY order — clients pick the first supported option (Smithy
// @auth is priority-ordered; the Smithy compiler materializes the alphabetical default when
// @auth is absent). Compilers whose source has no ordering semantics emit source order.
// An empty option (AuthRequirement{Schemes: []}) inside a non-empty list means "no auth is one
// acceptable choice" (TypeSpec NoAuth in a union, Smithy @optionalAuth, OpenAPI's empty security
// requirement) — distinct from Operation.Auth = [] (explicitly public).
```

## 10. Servers

```go
type Server struct {
    Name        Naming                  // servers are named entities (AsyncAPI name-keyed servers,
                                        // OpenAPI 3.2 Server.name)
    URLTemplate string                  // may contain {variables}
    Description Docs
    Variables   []ServerVariable        // {Name, Default, Enum []string, Docs, Unmodeled}
    Protocol    string                  // "https" default; "kafka", "wss", … for messaging servers
    ProtocolVersion string              // e.g. Kafka "3.5", AMQP "0-9-1" (AsyncAPI)
    Tags        []string
    Auth        []AuthRequirement       // server-scoped security — AsyncAPI's primary auth placement
                                        // (broker connections authenticate per server; different
                                        // servers of one service may require different schemes)
    Bindings    map[string]RawConfig    // server-level protocol bindings kept raw
    Unmodeled   Unmodeled
}
```

## 11. Versioning & availability

```go
type Availability struct {
    Added      []string  // version labels from Document.Versions, ordered — multiple entries
    Removed    []string  // support add/remove/re-add cycles (v1 present, v2 removed, v3 re-added)
    Deprecated string
    RenamedFrom     []VersionedName // {Version, Name}
    TypeChangedFrom []VersionedType // {Version, Type TypeRef} — property/return type changed
                                    // across versions (TypeSpec @typeChangedFrom)
    RequiredChanged []VersionedBool // {Version, WasRequired} — optionality flipped at a version
                                    // (TypeSpec @madeOptional/@madeRequired); the slice pass
                                    // reconstructs Required for older snapshots from it
}
type VersionedBool struct { Version string; WasRequired bool }
```

The IR stores the **timeline** (TypeSpec model); the `version-slice` pass produces a concrete
snapshot document per version for consumption. Emitters always receive snapshots — they never
interpret availability themselves. Formats without versioning semantics simply leave this nil.

`Availability` attaches everywhere versioning decorators can: `TypeCommon`, `Property`,
`Operation`, `Parameter`, `EnumMember`, `Variant`, and `OperationGroup` — a versioned enum
member or union variant must disappear from pre-`Added` snapshots. Smithy `@since` (free-form,
no version registry) lowers to `Added` only when the compiler synthesizes labels; otherwise it
is kept verbatim under `Unmodeled`.

## 12. Docs, deprecation, examples, unmodeled constructs

```go
type Docs struct {
    Summary     string
    Description string        // CommonMark; may contain {t:TypeID} cross-reference tokens that
                              // emitters resolve to language-appropriate links (Kiota's doc templates)
    ExternalDocs []Link       // {URL, Description}
}

type Deprecation struct { Message, Since, RemovalVersion string }

type Example struct {
    Name        string
    Summary     string
    Description string
    Value       *Value        // single-value examples (schemas, properties, parameters);
                              // for message examples: the payload
    Headers     *Value        // message examples: the correlated header values (AsyncAPI message
                              // examples are header+payload PAIRS — never split them)
    Input       *Value        // operation scenarios: paired input ↔ output/error
    Output      *Value        // (Smithy @examples, TypeSpec @opExample parameters/returnType)
    Error       *ErrorExample // the scenario ends in this error instead of Output
    ExternalURL string
    Unmodeled   Unmodeled
}
type ErrorExample struct { Type TypeRef; Content Value }
// Field legality is contextual (validated): Value/Headers on types, properties, parameters,
// contents, and messages; Input/Output/Error on operations. An Example never mixes the two arms.

type UnmodeledReason string     // vendor_extension | validation_only | degraded_lowering |
                               // no_ir_home | out_of_scope

type UnmodeledEntry struct {
    Reason     UnmodeledReason    // why this is here rather than modeled
    Value      RawValue          // the source construct, verbatim
    Provenance Provenance        // where the construct itself was written
}

type Unmodeled map[string]UnmodeledEntry
// keys are namespaced by origin: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "typespec:@myOrg/decorator", "asyncapi:bindings.kafka", "erlang:opaque"
// RawValue is the source JSON preserved verbatim.

type RawConfig map[string]RawValue
// declared protocol configuration the IR models a field for but leaves untyped inside:
// Server/Channel/Message/MessageBinding `Bindings`, `ProtocolDecl.Options`
```

`Unmodeled` is the lossless escape hatch: any source construct without a first-class IR node
survives, namespaced so two formats never collide, and typed passes can promote well-known
entries (`x-ms-pagination` → `Pagination`) without losing the original. The name states the one
property every member shares — the IR does not model it — rather than borrowing one format's
word for the concept, and rather than naming the guarantee: surviving verbatim is what the field
promises about an entry, but being unmodeled is why the entry is there at all, and `Reason`
records which flavour of unmodeled it is. OpenAPI/AsyncAPI say
*extensions*, Protobuf *options*, GraphQL *directives*, Smithy *traits*, TypeSpec *decorators*,
Erlang *module attributes* — and Protobuf's own `extensions` means something else entirely
(reserved field-number ranges), which the IR models as `Model.ExtensionRanges` and
`Property.ExtensionOf`.

Each entry carries two things beyond the value. **`Reason`** says why the construct is
unmodeled, so a consumer can take the subset it cares about: the validation emitter §4.7
anticipates wants `validation_only` and not vendor noise, while a spec linter wants
`degraded_lowering` and `no_ir_home` and nothing else. It is a property of the construct, never
of the diagnostic emitted beside it — one diagnostic code routinely spans preservation,
weakening, and outright dropping, so it says nothing about why. **`Provenance`** locates the
construct itself rather than the node carrying it, so an emitter can report on an entry at its
own source position instead of the enclosing schema's — falling back to the declaring position
only where the entry combines several keywords into one synthesized object and no single node
addresses it (`openapi:if-then-else`, `openapi:contains`, `openapi:unevaluated`).

| Reason | Meaning |
|---|---|
| `vendor_extension` | The source format assigns the key no semantics at all (`x-*` and every format's equivalent). The key set is unbounded and nothing can be inferred from the value. |
| `validation_only` | Validation logic rather than data shape: the structural picture is complete without it, and no target type system or sibling source format has an equivalent (§4.7). |
| `degraded_lowering` | No faithful target for the shape as written — the IR has no combinator for it, or no target type system holds it — so it was lowered to a documented weaker shape with the original kept beside it (§4.8). |
| `no_ir_home` | Expressible in target languages, and no boundary the IR draws on purpose: the position simply has no field for it yet. A gap expected to close — each of these is a promotion candidate. |
| `out_of_scope` | A boundary the IR draws on purpose (§15): runtime machinery or per-target SDK policy rather than API shape. Kept so nothing is lost, but no node is coming — the intended consumer is an emitter policy layer, not a promotion pass. |

`no_ir_home` and `out_of_scope` are the pair worth keeping straight, because they read alike and
mean opposite things: the first is an admission (a field the IR should probably grow), the second
is a decision (§15's list, which exists precisely so those constructs are never mistaken for
oversights). A promotion pass mining the IR for its next typed node reads `no_ir_home` and must
skip `out_of_scope`; nothing about the key or the value distinguishes them, which is the whole
reason `Reason` is data rather than a comment.

Sorting entries into *typed* buckets instead was considered and rejected: the defining property
of an entry is that it is unmodeled, so anything with enough known semantics to be sorted into a
bucket has enough to be modeled properly, and sorting would make the compiler guess — a
heuristic, which invariant 6 then requires to be injectable, marked `Inferred`, and disableable.
`Reason` gives the same discrimination as data, and callers already tell formats apart by key
prefix.

For the escape hatch to hold, **every node that can carry source metadata has an `Unmodeled`
field** — including `Response`, `ErrorCase`, `Payload`, `Content`, `Example`, `ServerVariable`,
`OAuthFlow`, and every binding struct (new spec revisions land fields on exactly these objects).
Per-file metadata (protobuf file options) is keyed by path in `Document.Unmodeled`.

`RawConfig` is the neighbouring map type, not a second escape hatch: it holds declared protocol
configuration the IR models a field for and deliberately leaves untyped inside (AsyncAPI
bindings at server, channel, operation and message level; `ProtocolDecl.Options`). Its values are
bare `RawValue`, not `UnmodeledEntry` — those entries are there because the source declared them
where the IR expects them, so there is no reason to record and no unmodelled construct to locate.

### 12.1 One structural home per declaration

Documentation, deprecation, XML hints, examples, vendor extensions, validation-only keywords and
value constraints are **position-scoped**: they bind the position they were written at, not the
shape that position resolves to. A source schema that reduces to a shared node — the `string`
primitive, the schemaless top type, or another type reached through a `$ref` — must therefore
never write them onto that node, because every other position resolving to the same shape would
then read them as its own.

Each such declaration gets exactly **one** home, chosen by the position rather than by what the
body lowered to:

| the position | where the declaration's annotations live |
|---|---|
| carries an `ir.Property` or `ir.Parameter` of its own — a model property, a header, an operation parameter | that carrier |
| carries neither — a component, an `items`, `additionalProperties`, `prefixItems[n]`, a `patternProperties` value, a union branch, a media-type or item schema, a `$ref`-hoisted sub-schema | a type node the position owns, hoisting a `Scalar` alias over the shared target when the body reduced to one |

Two consequences a compiler must hold to. **A carrier position never hoists an alias**: a
documented scalar field keeps its description on `Property.Docs`, where an emitter rendering that
field reads it, rather than on an anonymous node the field points at. **A non-carrier position
hoists only when the declaration says something** one of those homes holds — a bare
`items: {type: string}` gains nothing by owning a node, so it keeps resolving straight to the
shared primitive and the type graph stays free of an anonymous node per element position.

Where a carrier holds annotations of its own alongside a schema's — a header and a parameter both
write a `description` beside their schema — the carrier's own win: they describe that input,
while the schema's describe its type.

Constraints follow the same rule with one exception on the carrier side. `Property.Constraints`
and `Parameter.Constraints` are filled whether or not the schema hoisted a node, because no node
such a schema can hoist holds them: an object lowers to a `Model` and a formatted scalar to a
`Scalar` carrying an `Encoding`, and neither has a field for the scalar bounds.

## 13. Provenance & diagnostics

```go
type Provenance struct {
    Source   int       // index into Document.Sources, or NoSource (-1)
    Pointer  string    // JSON pointer or line:col into that source
    Inferred string    // "" = declared; else the heuristic that produced this node ("pagination-name-match")
}

const NoSource = -1 // the node addresses no input file

type Diagnostic struct {
    Severity   Severity  // error | warning | info
    Code       string    // stable: "openapi/unresolved-ref", "ir/dangling-type-ref"
    Message    string
    Provenance Provenance
}
```

Everything heuristic is auditable; everything broken is reportable with an exact source location.

The one exception is what `NoSource` exists for. A pass reporting on the document it was handed has
no input file to name, and every real index — `0` included — names a file the document loaded, so
reusing one would make a renderer fabricate a location. `NoSource` is therefore the **only**
out-of-table `Source` value the IR declares: a verifier accepts it and reports every other index
that addresses no declared source, so a producer inventing a second sentinel is caught rather than
tolerated.

---

## 14. Compiler lowering summaries

How each format's distinctive concepts land in the IR (full details live with each compiler):

| Format | Lowering highlights |
|---|---|
| **OpenAPI 3.x** | components/schemas → registry (IDs from pointers); inline schemas hoisted with hints; `allOf` → Base/Mixins per §4.3; `oneOf`/`anyOf` → Union (Exclusive bit), null-variant → Nullable ref, co-declared with structural keywords → the composition distributed across the variants per §4.3, or — for the five shapes that cannot be distributed — structural body + verbatim union per §4.8 (branches that declare no shape at all are `validation_only` per §4.7); competing keywords at one position — `const`/`enum`/`allOf`, elected in that order, and `oneOf` beside `anyOf`, where oneOf wins — lower as the elected keyword with every passed-over one verbatim Unmodeled (`degraded_lowering`) at its own pointer per §4.8, and a `{X, null}` oneOf beside an anyOf stays a Union rather than collapsing to a nullable ref; `discriminator` → Discriminator (3.2 `defaultMapping` → Discriminator.Default); `nullable`/type-arrays → Nullable; readOnly/writeOnly → Visibility and schema-level `default` → the referencing Property/Parameter's Default: both bind a *use* of the type rather than the type, so a declaration-site one is pushed down to referencing properties with use-site precedence and the declaration keeps its own copy verbatim (`no_ir_home` Unmodeled + diagnostic) — which is what a component nothing references would otherwise lose silently; `additionalProperties: false` → Additional=closed, `unevaluatedProperties: false` → closed_after_composition, `minProperties`/`maxProperties` → Model.Constraints (the property set's cardinality, as against Additional's openness); parameters → Params + HTTPBinding locations w/ style/explode, `allowEmptyValue` → Parameter.Unmodeled (`no_ir_home`: HTTPParamBinding holds its neighbours but not this one), a header parameter named Accept/Content-Type/Authorization lowered as declared + `reserved-header-name` warning (OpenAPI says such a definition SHALL be ignored; dropping declared content is an emitter's call, not a compiler's), 3.2 `in: querystring` → querystring location; requestBody/responses all content types → Payload.Contents, a non-required requestBody → Payload.Unmodeled (`no_ir_home`, presence is the IR's only body optionality); 3.2 `itemSchema` → Content.Item and `itemEncoding` → Content.ItemEncoding — except beside a positional `prefixEncoding`, where both go to Content.Unmodeled (`no_ir_home`) because a single every-item encoding cannot state ordinals; per-status responses/default → Conditions + ranges, error-response `headers` and its `content` map whatever its arity → ErrorCase.Unmodeled (`no_ir_home`, ErrorCase has neither field: it holds one TypeRef and no media type, so one entry loses the key it was written under just as several lose all but the first); response/encoding header `style` and `explode` → Property.Unmodeled (`no_ir_home`, ir.Property has neither field), and a `Content-Type` entry in either headers map lowered as declared + the same `reserved-header-name` warning the parameter position gets; webhooks → HTTPBinding.IsWebhook; callbacks → Callbacks; links → Response.Unmodeled (`no_ir_home`, promotable later); path-item `servers`, under `paths`, `webhooks` and a callback expression alike → Operation.Unmodeled (`no_ir_home`: §10 scopes servers by index list at service and channel, and an operation has no such list yet), with an operation's own `servers` — which OpenAPI says override the path item's — kept beside them under `openapi:operationServers`, the one key here not named for the keyword it holds, since two declarations at two pointers cannot share one map key without the survivor depending on lowering order; a path item's own `summary`/`description`, at the same three mounts → Operation.Unmodeled under `openapi:pathItemSummary`/`openapi:pathItemDescription` (`no_ir_home`) rather than merged into Docs: ir.Docs holds the operation's own pair and a path item's documents the path, so merging would need a precedence rule and would attach documentation the operation's author never wrote — an inference, which §6 places in policy rather than in a lowering; every operation a path item declares — the fixed method fields, 3.2 `query`, and 3.2 `additionalOperations` keyed by method — → an Operation apiece, mounted at its own pointer, with the `additionalOperations` key used verbatim as HTTPBinding.Method since OpenAPI reads a method name case-sensitively, and a key naming no method at all lowered as declared + `invalid-method-key` warning (the binding is unusable, but dropping the entry would lose every operation it declares); securitySchemes/security → Auth OR-of-ANDs, 3.2 device flow + `oauth2MetadataUrl` → Flows/OAuth2MetadataURL; servers+variables (3.2 named) → Servers; tags (3.2 parent/kind) → groups + TagDefs; info contact/license → Document; schema `example(s)` → Examples; `xml` object (incl. 3.2 nodeType) → XMLHints at type and property level; `not`/`if-then-else`/`dependentSchemas`/`dependentRequired`/`contains`/`propertyNames`/`unevaluated*` → verbatim Unmodeled per §4.7; `contentEncoding`/`contentMediaType` → `Encoding` on the scalar the position lowers to and `contentSchema` → Unmodeled (`no_ir_home`) per §4.7; `$id`/`$schema`/`$vocabulary` → Unmodeled (`out_of_scope`, `$id` not honoured for resolution); `$dynamicRef` → the anchored type by compiler expansion, else verbatim Unmodeled with the reason it was irreducible; an inline `allOf` branch declaring more than the merge consumes → verbatim Unmodeled (`degraded_lowering`) per §4.8; a boolean `false` `allOf` branch → the composed Model closed, branch verbatim Unmodeled (`degraded_lowering`) per §4.8, a `true` branch a silent no-op; a shape applicator (`properties`/`patternProperties`/`additionalProperties`/`required`/`items`/`prefixItems`, and `format` where no type is declared) the lowered node has no field for → verbatim Unmodeled (`degraded_lowering`) per §4.8; a parameter schema's `xml` and its `readOnly`/`writeOnly` → Parameter.Unmodeled (`no_ir_home`: Parameter has no field for either); `patternProperties` → AdditionalProps.Patterns; `prefixItems` → Tuple, with any trailing `items` → Tuple.Unmodeled (`degraded_lowering` per §4.8: an open tuple has no IR combinator, so the fixed head is lowered and the tail kept beside it); `x-*` → namespaced Unmodeled (legal on every object — hence Unmodeled on every node); `$ref`-adjacent sibling keywords (3.1) and ref-target annotations merge onto the referencing Property/Parameter with **use-site precedence**, applied uniformly (oagen's ad-hoc per-site patching is the counterexample), and at a position carrying no Property/Parameter — an `allOf`/`oneOf`/`anyOf` branch, `items`, a component — bind an alias hoisted at that position instead, per §4.3; a oneOf/anyOf whose variants are all string consts normalizes to a closed `Enum` in a `pass/` normalization — not in the compiler — so per-variant `Docs` survive until the collapse is chosen; mutually-exclusive parameter groups (`x-mutually-exclusive-parameter-groups`) stay as namespaced Unmodeled entries, and their documented *promotion* (no dedicated node needed) is a pass that synthesizes one logical `Parameter` typed by a `Union` of variant models, bound via `HTTPParamBinding.ParamPath` per field; pagination only via injectable policy, marked Inferred |
| **Swagger 2.0** | lifted to OpenAPI 3.x shape first (body/formData → Payload; host/basePath/schemes → Servers; consumes/produces → content types), then the OpenAPI lowering runs |
| **TypeSpec** | consumed post-check (monomorphized, `isFinished`); template instances → TypeCommon.Instantiation incl. value args → TemplateArg; models → Model w/ Base + spread provenance → Mixins; scalars → Scalar chains, constructors in values → Value.Ctor; `@encode`/`@format` → Encoding triple; `@encodedName` → WireNameByFormat at property AND type level; unions w/ named variants → Union, `@discriminated` → Discriminator.PropertyName/Envelope/EnvelopeValueName; `| null` → Nullable; visibility classes (incl. custom, `@invisible` → Visibility.None) → Visibility, op overrides → ParameterVisibility/ReturnTypeVisibility; `@patch` implicitOptionality → HTTPBinding.PatchImplicitOptionality; interfaces → OperationGroups (versionable); `@overload` → OverloadOf; `@sharedRoute` → SharedRoute; `@service` → Service; versioning decorators incl. `@typeChangedFrom`/`@madeOptional`/`@madeRequired` and add/remove cycles → Availability timeline (on members/variants/params too); pagination decorators incl. prev/first/last links and header continuation tokens → Pagination PropPaths (In:"header"); Azure.Core `@pollingOperation`/`@finalOperation` → LongRunning; multipart w/ parts → Content.Encoding/PartEncoding, `Http.File` → FileInfo (content-type set, contents chain, filename location); streams/SSE → StreamDetail + Variant.Event (contentType, terminal); `@error` → UsageFlags.Error; `@example`/`@opExample` → Examples (Input/Output pairs); `@pattern` message → Constraints.PatternMessage; `@mediaTypeHint` → TypeCommon.MediaTypeHint; `never` members deleted + diagnostic per §4.8; TCGC client-shaping decorators (`@clientName`, `@access`, `@usage`, `@scope`, `@override`, …) → namespaced Unmodeled (`out_of_scope`) consumed by emitter policy, never IR semantics; values/consts incl. enum-member refs → Values channel |
| **Smithy 2.0** | structures → Model, mixins → Mixins (non-structure mixins flattened — spec-sanctioned); `document` → Any; unions → WireTagged Union, member `@jsonName` → Variant.WireName; enum/intEnum → Enum (open by default); `@sparse` → element Nullable; traits: constraints → Constraints, `@paginated` → Pagination (declared), `@retryable` → ErrorCase.Retryable + Throttling, `@error` fault → ErrorCase.Fault, `@readonly` → Idempotency safe, `@idempotent`/`@idempotencyToken` → Idempotency, `@sensitive` → Sensitive/Secret, `@tags` → Tags, `@clientOptional`/`@input` → Property.ClientOptional (+InputOnly), `@addedDefault` → DefaultAdded, root-shape `@default` pushed down to properties w/ provenance; `@streaming` blob → StreamDetail (+`@requiresLength` → RequiresLength); event streams → StreamDetail.Events union + Property.EventHeader/EventPayload + Initial messages; service-level errors → Service.CommonErrors; protocol traits → Service.Protocols; service `rename`/`version` → Service.Renames/Version; resources → OperationGroup + ResourceInfo (identifiers, properties, lifecycle incl. put/@noReplace, instance vs collection ops); http traits → HTTPBinding incl. `@endpoint`/`@hostLabel` → HostPrefix/host location (additive binding), `@httpPrefixHeaders`/`@httpQueryParams` → Prefix bindings, `@httpResponseCode` → Response.StatusCodeProp, `@requestCompression` → Compression, `@httpChecksumRequired` → ChecksumRequired; `@auth` order → priority-ordered Auth, `@optionalAuth` → empty option; `@jsonName` → WireName; `@mediaType` → Encoding.MediaType; xml traits → XMLHints at type and property level; `@examples` → Examples (Input/Output/Error); waiters + rules-engine traits → verbatim Unmodeled (`out_of_scope`, §15); `smithy.api#Unit` → nil payload / shared empty Model for tag-only variants; other traits → namespaced Unmodeled |
| **GraphQL** | §8.4; interfaces (incl. interface hierarchies) → Abstract models + Implements; field arguments → Property.Args; input objects → `InputOnly` models, `@oneOf` inputs → WireTagged Exclusive Union; non-null wrapping → Required/Nullable (all `[T!]!` combinations via per-layer list nodes); defaults on args AND input fields → Default (list-input coercion normalized); custom scalars → Scalar{Base: nil}, `@specifiedBy` → Unmodeled; deprecation w/ reason at every location incl. args/input fields; directives → ordered-array Unmodeled convention + document-level definitions |
| **AsyncAPI** | servers w/ protocols/protocolVersion/security → Servers (named, w/ Auth); channels → Channels (Address nil = unknown), messages → Document.Messages registry referenced by ID; operations send/receive → Operation + MessageBinding, no-reply sends → OneWay; reply objects → Reply (static channel + dynamic PropPath address + message set); correlation IDs → PropPath (In: header|payload); message headers → hoisted header model (Headers *TypeRef); message examples → Example{Headers, Value} pairs; multi-format payload schemas → Content.SchemaFormat + verbatim schema in Unmodeled (Avro payloads lower through the type graph: record→Model, enum→closed Enum w/ FallbackMember, fixed→bytes w/ length, decimal→Constraints.Precision/Scale, aliases→Naming.Aliases); parameter `location` → Parameter.ValueFrom; traits applied by merge w/ provenance; protocol bindings at server/channel/operation/message level → raw Bindings maps; payload JSON-Schema lowering shared with OpenAPI |
| **Protobuf** | messages → Model w/ WireIDs; oneof → WireTagged Exclusive Union w/ variant WireIDs + Flatten on the synthetic wrapper property (oneof members are top-level wire fields; synthetic oneofs from proto3 `optional` do NOT lower to unions — they are presence markers); presence disciplines → Property.Presence (never Nullable); enums → Enum open/closed per syntax/edition, allow_alias → duplicate member values; map/repeated → MapT/List (packed/expanded → List.Encoding); groups / editions DELIMITED → nested Model + Encoding "delimited"; scalar wire variants (sint/fixed) → Encoding; `extend` fields → Property.ExtensionOf, extension ranges → Model.ExtensionRanges; well-known types → External (wrapper types → External or nullable primitive per injectable policy); custom options → namespaced Unmodeled (proto-JSON), file options keyed by path in Document.Unmodeled; services/rpcs → Service/Operation + RPCBinding; streaming modifiers → StreamingMode; gRPC transcoding (google.api.http) → additional HTTPBinding entries (slice), path patterns → PathPattern, nested bindings → ParamPath, response_body → ResponseBodyPath; reserved ranges → Unmodeled (guarded by validate pass) |
| **Erlang/OTP** | §8.5; `-type`/`-spec` type language → type graph: tuples → Tuple, tagged-tuple unions → Union + Discriminator.Index, records → Model{Positional} w/ 1-based WireIDs, atoms → symbol Values / Literal types / `Scalar{Base: string, Encoding: "erlang:atom"}`, integer ranges → integer Scalar + Min/Max, maps `:=`/`=>` → Properties(Required)/AdditionalProps w/ typed keys, `pid()`/`port()`/`reference()`/`fun()` → External, `string()` → List of char scalar, parametrized types monomorphized w/ Instantiation; behaviours: gen_server call/cast → Operations + OTPBinding (cast → OneWay), info → channel Messages received, gen_event → Channel w/ N messages, gen_statem states → `Unmodeled["otp:states"]`; registered process → Channel.Address, registration kind → Channel.Bindings["otp"]; reply-union Errors split → injectable Inferred policy; bit-sized binaries/map assoc lists/`-opaque` → §4.8 degraded lowerings; `-deprecated` → Deprecation; `-doc`/EDoc → Docs |

## 15. Deliberate exclusions

Constructs on this list that a source format actually declares are still kept — losslessness is
not negotiable — but they are kept under `ReasonOutOfScope` (§12), never `no_ir_home`: the
boundary is drawn on purpose, so no promotion pass should read them as a gap to close. Entries
here that no format declares (language casings, generator plan artifacts) are simply absent from
the IR and preserve nothing.

- **Language names/casings** — emitters own identifier rendering entirely (IR stores neutral
  word sequences + wire names).
- **SDK runtime policy** (retry/timeout/telemetry/error-class taxonomy) — a separate emitter
  input, never IR (§2.4 of architecture.md). OTP call timeouts and `multi_call`/`send_request`
  machinery fall here too.
- **Generator plan artifacts** (request builders, executor pairs, per-visibility model variants,
  primary-response selection) — computed views in emitters, not stored.
- **Structured modeling of transport-deployment minutiae** (Kafka partition configs, AMQP
  exchange args) — preserved as raw namespaced entries.
- **Smithy rules-engine traits** (`@endpointRuleSet`, context params) — runtime endpoint
  resolution machinery; preserved as raw namespaced entries on the service.
- **Smithy waiters** (`smithy.waiters#waitable`) — preserved verbatim in `Operation.Unmodeled`,
  never folded into `LongRunning`; promoted to a typed node only if a second format lands.
- **TCGC-style client-shaping decorators** (`@clientName`, `@access`, `@usage`, `@scope`, …) —
  per-language SDK-surface policy, preserved as namespaced entries for emitter policy layers.
- **Arbitrary GraphQL persisted queries** — the type graph and entry points are retained; query
  composition is a emitters/runtime feature.
- **Capability/interface-typed fields** (Cap'n Proto capability passing) — requires
  services-as-types; out of scope for a data-SDK compiler.
- **Function types** — Erlang funs degrade per §4.8; no target language marshals closures.
- **A general intersection combinator** (an `Intersection` node, or an `Intersect` sum arm, for
  the *struct ∧ union* shape a JSON Schema `oneOf` beside structural keywords writes) — asked and
  answered, so it does not need re-litigating. The capability is not cross-format: only OpenAPI
  and AsyncAPI can express it, and they share a compiler code path, so a node would be one
  format's artifact given IR-wide status against invariant 9. TypeSpec's `&` is *model-only*
  (`ir-spec-matrix.md`'s Intersection row records it as `⚠ & (model is)`) and so is already
  covered by §4.3's `Base`/`Mixins`; Smithy, GraphQL, Protobuf and Erlang/OTP cannot express it
  at all. The shape is instead handled where it arises: distributed across the union's variants
  per §4.3 when every branch names a referent, and kept verbatim under `ReasonDegradedLowering`
  in the five residual shapes §4.8 lists. A second format that declares a genuine intersection —
  not a model-only `&` — is what would reopen this.

## 16. Open questions (tracked for implementation)

1. **BigVal representation** — decimal string wrapper vs `math/big` types at the boundary;
   leaning decimal-string in the IR, `math/big` in helpers.
2. **Dedup aliasing mechanics** — alias table on Document vs ID rewriting with a redirect map;
   leaning alias table (IDs immutable, always).
3. **Content negotiation in the plan layer** — the IR keeps all media types; the default
   primary-selection policy (json > form > multipart > binary?) belongs to emitters but should be
   shared as a standard plan helper.
4. **Whether `UsageFlags` is stored or always recomputed** — leaning: computed by a standard pass
   and stored, since emitters all need it and it's expensive to derive.
5. **Dual identity for cross-revision correlation** — pointer-derived `TypeID`s are stable across
   runs but churn when a schema moves within its source file. TCGC solves cross-version
   correlation with name-based `crossLanguageDefinitionId`s. Likely resolution: keep pointer IDs
   as the structural identity and add a derived, name-based correlation key computed by a pass —
   decide once IR diffing between spec revisions is actually built. Evidence from oagen's
   differ/compat stack: (a) diffing IR-document-to-IR-document is the right plane (oagen already
   does this); (b) name-based correlation alone still misses renames — plan a third *structural*
   tier (content-hash / field-set similarity, shared with the `dedup` pass) as the fallback when
   both pointer and name churn. Without stable identity, rename detection costs hundreds of
   lines of best-effort heuristics downstream (see prior-art.md §1).
6. **Response discrimination placement** — `ResponseConditions` (status ranges) is HTTP-shaped
   living on the neutral core. Fresh evidence that it sits awkwardly: RPC single-response and OTP
   call replies both use empty conditions, and `Response.StatusCodeProp` is likewise
   HTTP-flavored. Alternatives: move conditions into `HTTPBinding` alongside parameter locations,
   leaving responses as an ordered named list. Revisit when the RPC or messaging compiler lands
   and either validates or strains the current shape.
7. **Semantic nullability (GraphQL)** — the `@semanticNonNull` RFC ("null only on error") is a
   third nullability state `TypeRef.Nullable` cannot express. Pre-spec today: preserved as
   `Unmodeled["graphql:@semanticNonNull"]`. If it merges into the spec, add a `NullKind` (or a
   `NullOnlyOnError` bool) rather than churning every compiler early.
