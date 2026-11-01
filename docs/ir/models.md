# Models

Part of the [IR design](./README.md). Structs, objects and messages: what a model declares, what it
composes, and how a consumer reads the shape the two make together.

```go
type Model struct {
    TypeCommon
    Properties     []Property
    Requires       []string        // wire names of members an instance must carry that no own
                                   // property declares: one Base, Implements or Mixins declare,
                                   // which this model makes required, or one only the catch-all
                                   // governs (JSON Schema `required`); none names an own property
    Base           *TypeRef        // declared single inheritance (TypeSpec extends, allOf-as-inheritance)
    Implements     []TypeRef       // interface conformance, N-ary (GraphQL `implements A & B`); targets are Abstract models
    Mixins         []TypeRef       // composition without subtyping (Smithy mixins, TypeSpec spread provenance, extra allOf entries)
    AdditionalProps *AdditionalProps // catch-all beside the declared structure; a catch-all
                                     // with nothing beside it is a MapT, never a Model (docs/ir/containers.md)
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
    Key      *TypeRef       // nil = string keys, the one spelling of them (docs/ir/containers.md)
    Patterns []PatternProps // key-pattern-scoped value schemas (JSON Schema patternProperties)
}
type PatternProps struct { Pattern string; Value TypeRef }
```

## Discriminator

```go
type Discriminator struct {
    // Exactly one of Property / PropertyName / Index locates the tag:
    Property PropID                // model hierarchies: the property that carries the tag, the
                                   // declaration this model's flattening takes for it
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

## Composition

`Properties` contains only the model's *own* properties. Consumers walk
`Base`, `Implements`, and `Mixins` for the full shape; the flattening is computed, never stored.
`ir.FlattenedProperties` computes it, by the rules under *The flattening* below; `ir.ExposedProps`
computes the flat set of property IDs it reads, and `ir.Supertypes` and `ir.IsSubtype` the subtype
relation, which excludes mixins; `irfacts.Index` answers the exposed set and the subtype relation
for many questions at once ([derived-facts](./derived-facts.md)). This preserves what oagen and
Kiota lose: the difference between "inherits from", "conforms to", "mixes in", and "declares".
`Base` is single (subtype identity); `Implements` is N-ary conformance to `Abstract` models — the
distinction matters because GraphQL allows multiple interfaces while every class-based target
language allows at most one base class.

## Required members

Whether an own property must be present is its
`Required` ([properties](./properties.md)). A member the model requires that none of its own
properties declares is in `Requires`, by wire name and once (`irverify` refuses a repeat as
`ir/requires-duplicate`), in the order the source's `required` lists name it: an inherited property
the model makes required, as `allOf: [{$ref: User}, {required: [id]}]` makes `User`'s `id` required
on the composed model, or a member only the catch-all governs, which JSON Schema requires whether or
not a schema declares it. The flattening resolves each name over the flattened shape (below), and
the IR asserts nothing about which of these a name is, since saying so needs the hierarchy walked. A
name an own property declares is never listed (`irverify` refuses it as `ir/requires-own-property`),
so required-ness has one home per member. A model that requires a member is neither a map
([containers](./containers.md)) nor a decorator ([annotations](./annotations.md#decorators)), since
a map requires no key and a decorator says nothing its `Base` does not.

## The flattening

`ir.FlattenedProperties(types, id)` returns a model's full shape: its members,
one per wire name, each with the declaration it takes, the declarations that one overrides and
whether an instance must carry it, beside the names a `Requires` lists that no member declares and
the composition entries that name no model. Its rules:

- *What it reads.* The declarations `ExposedProps` reaches and no others: the model's own, and those
  of every model its `Base`, `Implements` and `Mixins` lead to at any distance. Every reference, the
  root's included, is read through alias scalars, since a `$ref` carrying siblings composes the
  alias it hoists rather than the schema it names (below). A root naming no model has an empty
  shape.
- *Members.* Declarations are one member when their wire names are equal: `WireName`, else
  `Name.Source` (`Property.EffectiveWireName`, [properties](./properties.md)). The empty name is a
  name, the JSON member keyed by the empty string. A per-format name (`WireNameByFormat`) and an
  alias a property answers to (`Name.Aliases`) neither join members nor split one. A property
  several models declare under one `PropID`, as the variants of a distributed union do (below), is
  one declaration however often it is read.
- *Layout order.* The walk goes depth first from the root, following each model's `Base`, then its
  `Implements`, then its `Mixins`, each in order, and reads a model's own properties, in source
  order, once it has read every model the model composes that does not compose it in turn. Models
  that compose each other, a cycle the IR permits, are read together, in the order the walk
  finished them. So what a model composes comes before what it declares, conjuncts in the order the
  model lists them.
- *Override.* A later declaration of a wire name overrides an earlier one, and the member stays
  where its name was first read. So a model's declaration overrides that of every model it composes,
  at any distance, unless that model composes it too; and of two declarations whose models do not
  compose each other, the later conjunct's wins: a `Mixin`'s over the `Base`'s, a later `Mixin`'s
  over an earlier one's. A subtype narrowing its base's `petType` to `{const: dog}` keeps the base's
  slot with its own type. The member takes every facet but whether it is required — type,
  `Presence`, visibility, default, docs — from the declaration it takes, and lists the ones it
  overrides, which is where a consumer conjoining more finds them. Nothing is reported: redeclaring
  is how a subtype narrows, and whether two declarations agree is a question about their types,
  which the flattening does not ask.
- *Requiredness.* A member is required where any of its declarations is `Required`, or any model
  the walk reads lists its wire name in `Requires`: JSON Schema requires what any conjunct
  requires, so requiredness only accumulates. A name a `Requires` lists that no member declares is
  undeclared, a member only the catch-all governs.
- *Conjuncts that are no model.* A composition entry naming no model once read through alias
  scalars — a scalar, an enum, a union, a map, a list, or a type the registry does not hold —
  contributes no property, so the flattening lists the entry's target as written, once, for its
  consumer to lower (`Base` and `Mixins` name whatever the source composed with, below).
- *What it leaves.* A catch-all, `AdditionalProps` and `Additional`, is not flattened: a consumer
  reads the model's own ([TypePlan](../emitters/plan/types.md)), so a conjunct closing its property
  set is the consumer's to weigh. A property marked `Flatten` is one member like any other, the
  hoisting of its fields being its wire's ([properties](./properties.md)). Layout order is no wire
  order: a `Positional` model's elements follow their `WireID`.
- *Addressing a member.* A `PropID` naming a model's member, an encoding key in `Content.Encoding`
  ([payloads](./payloads.md)) or a discriminator's `Property`, names the declaration the flattening
  of that model takes, the content's type for an encoding key, so a consumer finds it on the member
  itself. The OpenAPI compiler keys a part through the flattening, and tags a discriminator by an
  own property, which the flattening takes. `pass.Validate` holds each only to a property the model
  exposes, not yet to the one it takes, since a flattening per model it asks about grows
  quadratically down a deep hierarchy, which its cost tests refuse.
- *Cycles and cost.* Each model and alias scalar is read once, so a cyclic composition is read to
  its fixpoint, each model of a cycle holding the members of all of them, and nothing is cut short:
  the walk needs no step budget and no recursion. It costs the declarations it returns plus the
  models, entries and aliases it passes, so an `irfacts` index could answer no faster, and it stays
  a function of `ir`.

Why the later conjunct wins: JSON Schema conjoins every `allOf` entry, so neither of two unrelated
declarations is wrong on the wire, and the later conjunct is where a spec writes the specialization.
On the golden spec (`cloudflare/api-schemas` `openapi.json` at `e2b8c60`, compiled at `efafa0d0`),
1,197 of the 3,866 composed models redeclare a wire name. Of the 1,811 overrides between
declarations whose models do not compose each other and whose types are different nodes, 1,788 are
made by a failure schema. They fall on 1,572 members, every one in a 4XX, 5XX or default response
that composes a success shape and, after it, the failure schema (`success: false`, `result: null`),
which an earlier-conjunct rule would type as the success. 774 members are required only through a
declaration they override, no `Requires` naming them, which a rule reading requiredness off the
declaration taken alone would make optional. Visibility is the declaration taken's because both
directions occur: of the 41 members whose declarations disagree on it, 28 take one that makes them
read-only, and 13 one that lifts the read-only an overridden declaration states, as a batch patch
body redeclares the `id` it must send, which a conjunction of the two would forbid writing.

## Composition entries

Which `allOf` entry becomes `Base` and which a `Mixin` is the OpenAPI compiler's rule
[`allof-classification`](../compilers/openapi.md#allof-classification).

A `$ref` entry that also writes keywords beside the `$ref` composes the *branch*, not the schema
the branch names: those keywords bind this use of the target and cannot go on the shared node, so
the branch position takes a node of its own — an alias over the target carrying them — and
`Base`/`Mixins` point at that. It is the same alias any other `$ref`-with-siblings position hoists
when it carries no `Property` or `Parameter` of its own to hold them — `items`, a union branch, a
component — so nothing about composition is special here; a `$ref` entry writing nothing beside
itself composes straight to the target and hoists no node. The alias's name hint is the hint the
node the `$ref` resolves to carries — a component's name, or, for a position inside a schema, the
hint its position gives it, an operation's or a response's included. A hint is a function of the
documents and the position alone, so an outside `$ref` naming the branch pointer gives the same one,
whichever lowering interns the node.

`Base` and `Mixins` name whatever the source composed with, which in JSON Schema is any schema:
`allOf: [{$ref: Pet}, {$ref: Tags}]` is legal and, `Tags` being a list, lowers to a `Mixin` pointing
at a `List`, and the same holds for a `Scalar`, an `Enum`, a `Union`, a `MapT`, or the top type. They
are typed `TypeRef` rather than "reference to a `Model`" for exactly that reason — narrowing them
would mean dropping a branch the source wrote, against invariant 2. What is *shaped* like `allOf` is
the composition itself: one referent per conjunct, resolved by reference. Emitters that can only
render model inheritance are the ones that must lower a non-`Model` conjunct to something their
target can express. An `allOf` none of whose conjuncts can make an object composes nothing: it is
[a use of one of them](#an-allof-no-object-satisfies).

## An allOf no object satisfies

A `Model` says its value is an object, and `allOf: [{type: integer}, {type: integer, format: int32}]`
admits none, so it is no composition. An `allOf` no conjunct can make an object lowers to one use of
the conjunct that decides it, by the OpenAPI compiler's rule
[`allof-refinement`](../compilers/openapi.md#allof-refinement): a reference to its `$ref`, a node at
an inline branch's own pointer, and an alias only where the position writes what no carrier holds.
At `cloudflare/api-schemas` @`319dc756` that is 517 of the 3,654 positions lowering an `allOf`, 408 of
them a single `$ref` entry.

Nothing here is the flattening the [design rules](./README.md#design-rules) leave to emitters. No
member of one node is copied into another, and no conjunct is lost: it is the reference the position
names, an annotation read where the same keyword beside the `allOf` is read, what the position
already says, or kept whole under its path, as a `$ref` a value set narrows is. An `allOf` whose
conjuncts can be objects composes as it always did, one sole `$ref` with nothing beside it a
decorator ([annotations](./annotations.md#decorators)) for emitters to collapse.

The designs this was weighed against:

- **A `Model` for emitters to collapse**, the decorator rule alone, which is how a wrapper around a
  non-object lowered until now, on the reasoning that collapsing one is a target's ergonomics
  (#255). That holds for a wrapper around an object, and a `Model` over a string is more than an
  inconvenience: every consumer reading `Kind`, the docs emitter and `pass` among them, is told an
  object the wire never carries. It also has nothing to collapse into where no `$ref` is written, as
  above, and a narrowed enum has no decorator form, since its value set is what the `$ref` does not
  say.
- **Rewriting the `Model` once the registry settles**: the census and each carrier have already
  read the position by then, as a node of its own.
- **An alias per branch**: `readOnly` and `default` bind a use, and an alias has no field that
  reaches a carrier.
- **The field merge over conjuncts**
  ([`allof-redeclared-property`](../compilers/openapi.md#allof-redeclared-property)): `items` and a
  component hold no `Property` to fold into, and a target's own docs would read as a conflict with
  the use's.
- **A schema synthesized from the conjuncts**: its `items` would hoist at pointers the source never
  wrote (invariant 3).
- **An alias for a narrowing**, as `{$ref: Op, enum: [set]}` lowers beside a 3.1 `$ref`: the field
  then keeps its value set only under `Unmodeled`, where the position has a typed `Enum` to hold it.

## Composition beside a union

JSON Schema conjoins sibling keywords, so
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
instead, enumerated by the OpenAPI compiler's rule
[`union-beside-structure`](../compilers/openapi.md#union-beside-structure).
