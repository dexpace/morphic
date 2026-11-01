# Containers and the rest

Part of the [IR design](./README.md). Lists, maps and tuples, which are real type nodes, and the
literal, external and schemaless kinds.

```go
type List struct    { TypeCommon; Elem TypeRef; Constraints *Constraints; Encoding *Encoding }
                    // Constraints: minItems/maxItems/uniqueItems.
                    // Encoding: container-level wire encoding — protobuf packed vs expanded repeated
                    // fields ("packed"/"expanded"); stacks with the element's own encoding
                    // (repeated sint64 [packed=false] = zigzag element + expanded list)
type MapT struct    { TypeCommon; Key *TypeRef; Value TypeRef; Constraints *Constraints }
                    // a map and nothing else: TypeSpec Record, a protobuf, Smithy or Avro map, an
                    // object whose one statement about its members is additionalProperties.
                    // Key: nil = string keys, the one spelling of them, as for AdditionalProps.Key.
                    // Constraints: minProperties/maxProperties (Smithy @length), the entry count
type Tuple struct   { TypeCommon; Elems []TypeRef; Rest *TypeRef; Constraints *Constraints }
                    // prefixItems + items, TypeSpec tuples, Erlang tuples
                    // Rest: the type of every element past Elems; nil = none may follow
                    // Constraints: minItems/maxItems/uniqueItems over the whole instance
type Literal struct { TypeCommon; Value Value }
                    // const, a source's literal types, Erlang atoms;
                    // a one-member enum is an Enum (docs/ir/enums.md)
type External struct{ TypeCommon; Identity string; Package string; MinVersion string } // resolve to a well-known library type (TCGC external)
type Any struct     { TypeCommon }                                         // schemaless
```

Containers are real type nodes with IDs (hoisted like all anonymous types), not flags on
references — Kiota's `CollectionKind` flag couldn't express nested collections or constrained
lists cleanly. `External` also carries opaque runtime handles that no target can structurally
model (`erlang:pid`, `erlang:fun` — see [boundaries](./boundaries.md#degraded-constructs)).

## Maps

**A map and nothing else is a `MapT`.** `Model.AdditionalProps` and `MapT` both type a value under
arbitrary keys, and what tells them apart is what else the source says about the members. A
catch-all beside any other member fact — a declared property, a pattern-scoped value, composition
(`Base`, `Implements`, `Mixins`), a closure (`Additional`), a discriminator or a subtype's tag, or
any other field only a `Model` has — is that model's `AdditionalProps`. A catch-all with nothing
beside it is a `MapT`, never a `Model` with no properties. TypeSpec's `Record<T>`, a protobuf map
and an OpenAPI object writing only `additionalProperties` are one construct, and a document that
held some of them as models would leave every emitter to recognize the map itself, the work a
dictionary node exists to spare it (prior-art: Kiota has no map type, TCGC has one). Both spell a
string key one way, as a nil `Key`. A key naming the `string` primitive states that type a second
way, so `irverify` rejects it (`ir/map-key-string`), and a `Key` that is set names some other type.

`ir.AsMap` states the rule field by field, and it is the only statement of it. A compiler builds the
`Model` its source describes and converts it through `ir.AsMap`, so a lowering that comes to fill
another `Model` field keeps the node a model with no second predicate to update, and `irverify`
holds every document to the same function (`ir/map-as-model`). The conversion keeps the model's
`TypeCommon` — its ID, name, docs and unmodeled entries — and takes the catch-all's key and value
and the model's `Constraints`, which on a map bound how many entries an instance may carry. A map
requires no key, so an object whose `required` names one is a `Model` whose `Requires` lists it
([models](./models.md#required-members)), never a `MapT`, however it is composed. What a map has no
field for is kept the way any keyword without a home is: `propertyNames` stays the validation-only
entry [boundaries](./boundaries.md#validation-only-constructs) makes it, since it constrains keys
without being a key type.

## Tuples

A `Tuple` states three independent things, one field each. `Elems` types the leading positions.
`Rest` types every element past them, as `List.Elem` types every element of a list; nil means no
element may follow, so an open rest that constrains nothing targets the top type rather than being
absent. `Constraints` bounds the whole instance, `Elems` and `Rest` together, under the same rules
as a `List`'s ([annotations](./annotations.md#what-a-reference-carries-down)).

`Elems` does not require its positions. JSON Schema's `prefixItems` requires none of them, and an
array has no holes, so the required positions are always a prefix, and `Constraints.MinItems` is how
long it is. A consumer reading a `Tuple` with no `MinItems` must therefore treat every position as
optional, and a source whose tuples are exact — TypeSpec's and Erlang's — lowers each with
`MinItems` equal to `len(Elems)` and no `Rest`. No field is derived from another: a compiler neither
trims `Elems` to `MaxItems` nor restates a closed tuple as a `MaxItems`.
