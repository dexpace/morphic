# The type graph

Part of the [IR design](./README.md). The flat registry of type nodes: the kinds, what every kind
shares, and how one node refers to another. Each kind has a document of its own:
[primitives](./primitives.md), [scalars](./scalars.md), [models](./models.md),
[unions](./unions.md), [enums](./enums.md) and [containers](./containers.md).

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
    Sensitive  bool         // whole-type redaction (Smithy @sensitive on shapes, OpenAPI format:password),
                            // shared by every type whose Base reaches this one; Property.Secret is the per-use form
    Access     string       // "" = public; "internal" = not part of the exported SDK surface
                            // (protobuf editions export/local, TCGC @access(internal), Smithy @internal via policy)
    Deprecation *Deprecation
    Availability *Availability
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

`Namespace` is a path, one segment per level, as `Service.Namespace` and `GraphQLBinding.FieldPath`
([graphql-binding](./graphql-binding.md)) are. **No segment is blank** (`ir/path-segment-blank`): no
namespace or field grammar has an empty level, so a source that spells one has written an invalid
name, which its compiler reports rather than carries. A null or global namespace is the empty list,
not one blank segment. A segment may repeat — `com.acme.acme` has three levels — so a path is not
held to unique entries.

**Derived facts** — the subtypes of a model, the properties a type exposes, how each type is used,
which types can hold themselves — are not stored on a type; `ir/irfacts` derives them from the
document ([derived-facts](./derived-facts.md)).

## Type references

```go
type TypeRef struct {
    Target   TypeID
    Nullable bool     // this *usage* admits null on the wire
    Inferred Inferred // "" = the source declares the null; else the heuristics that read it in (docs/ir/unmodeled.md)
}
```

`Target` is never empty. A position that admits no type — a model with no base, a stream with no
element type — holds a `nil *TypeRef` ([primitives](./primitives.md)), so a `TypeRef` naming nothing
is one a lowering left unfilled, and `irverify` reports it (`ir/type-ref-no-target`).

Nullability lives on the reference, not the target type, because the same type is nullable in one
position and not another. Combined with `Property.Required` this yields the four distinct states
(required/optional × nullable/non-null) that OpenAPI 3.1, TypeSpec, and GraphQL all distinguish.
Compilers normalize every source spelling to this one bit: OAS 3.0 `nullable: true`, OAS 3.1
`type: [T, "null"]`, TypeSpec `T | null`, GraphQL's absence-of-`!`.
A oneOf/anyOf/union whose only distinction is a null variant becomes a plain nullable `TypeRef`,
never a union node.

The bit describes the whole schema, not the keyword that spells it. Where a source format conjoins
keywords (JSON Schema), a compiler reads them together: a value set that omits `null` beside a type
that names it — `{type: [string, "null"], enum: [red, green]}` — does not admit null, one that lists
`null` with no type beside it does, and a composition admits what its conjuncts jointly admit. So
every spelling of one constraint reaches the same bit, which is the only thing an emitter reads.

A bit a heuristic set rather than the source declared names that heuristic in `Inferred` (invariant
6). It is the reference's own marker because a reference has no `Provenance`
([provenance](./provenance.md)) and the node holding one may hold several, so marking the holder
would not say which bit was guessed. The marker is set only where the heuristic admits null and the
source's own reading does not, so `Nullable && Inferred == ""` is the bit that reading sets.
`irverify` reports a marker on a reference that admits no null
(`ir/type-ref-inferred-not-nullable`). The OpenAPI compiler's one such heuristic is its
nullable-widening policy ([`nullable`](../compilers/openapi.md#nullable)).

Protobuf field *presence* is **not** nullability — protobuf has no null. Presence disciplines
lower to `Property.Presence` ([properties](./properties.md)), keeping `Nullable` strictly about
wire-null.
