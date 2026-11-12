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
    Additional     AdditionalMode  // openness beyond declared + the catch-all's patterns
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
    DiscriminatorValue string      // set on each subtype one tag routes to: that wire tag value
    InputOnly      bool            // GraphQL input types; distinct identity from output types
}

type AdditionalMode string
// ""                         — unspecified (open by JSON Schema default)
// "closed"                   — no properties beyond the declared set and the catch-all's patterns
//                              (additionalProperties: false, closed-by-construction records)
// "closed_after_composition" — closed once composition is resolved (unevaluatedProperties: false)

type WireIDRange struct { From, To int }

type AdditionalProps struct {
    Value    *TypeRef       // a key no pattern matches; nil = no such key is allowed, only beside
                            // Patterns (docs/ir/models.md#catch-alls)
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
    Mapping  map[string]TypeID     // wire value → subtype; nil on a union = infer by type name;
                                   // a hierarchy routes to each subtype by its DiscriminatorValue
    Default  TypeID                // variant to use when the tag is absent/unrecognized
                                   // (OpenAPI 3.2 defaultMapping); zero = none
    Envelope string                // "" = tag inline in the variant object;
                                   // "object" = {kind, value} wrapper (TypeSpec @discriminated envelope)
    EnvelopeValueName string       // wire name of the envelope's value property (default "value";
                                   // TypeSpec envelopePropertyName); meaningful only when Envelope=="object"
    Inferred bool                  // discovered heuristically (each variant pins a property to a
                                   // distinct value), not declared; a heuristic filling only the
                                   // Mapping of a declared one leaves it false
}
```

A heuristic that sets `Inferred`, or fills the `Mapping` of a discriminator the source declares
with none, names itself in the `Provenance.Inferred` of the node holding the discriminator
([provenance](./provenance.md)), as every heuristic does. `Inferred` alone says which half was
read in: the tag itself, or only where its values route.

A subtype's `DiscriminatorValue` is the tag by which a discriminator on one of its ancestors routes
to it: a key that discriminator's `Mapping` names it under, or else the name the source routes it
by. It is empty on a model no one tag routes to, such as one no discriminator routes, as none routes
a schema composing a discriminated union that does not list it, each of its instances carrying the
tag of the variant it is ([below](#a-union-a-composition-names)). Where the subtype admits no
instance carrying the name it is routed by, a heuristic may read the tag off the strings it admits
instead, leaving the field empty where it admits several or none, and names itself in the subtype's
`Provenance.Inferred`. Two subtypes may carry one tag, where the instances of each carry only it, as
every subtype of a base pinning its tag to one value does, and that tag then routes to more than
one. A reading that declined there, as one filling a `Mapping`, which keys one target per tag, must,
would leave each a tag its own instances reject for an encoder to write.

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

## Catch-alls

`AdditionalProps` types the members a model does not declare, and `Additional` says whether any are
allowed. `Value` types a key no pattern matches, each pattern's `Value` a key it matches, and `Key`
every key the catch-all governs. Three combinations arise, and each says one thing about the keys
no pattern matches:

- **A value.** Such a key is allowed and its value is of `Value`'s type: an object writing
  `additionalProperties` as a schema, or as `true`, which types it as the top type.
- **Patterns and no value.** No such key is allowed, as `Tuple.Rest` is nil when no element may
  follow: an object writing `patternProperties` beside `additionalProperties: false`. The model is
  `Additional` closed as well, which says the same of every member beyond its declared ones and
  the patterns.
- **Patterns and a value.** Both hold: an object writing `patternProperties` and neither
  `additionalProperties` nor `unevaluatedProperties: false`, whose other keys JSON Schema admits
  whatever they hold, types the value as the top type.

A catch-all with neither a value nor a pattern admits nothing a closed model does not already say,
so it is not written: `irverify` reports one (`ir/catch-all-empty`). A closed model whose catch-all
has a value types keys its closure forbids, so a reader of the value and a reader of the closure
would admit different payloads: `irverify` reports that too (`ir/catch-all-closed`). A model closed
only after composition (`closed_after_composition`) may keep a value, since a catch-all the source
writes is itself what evaluates a key in JSON Schema.

## The flattening

`ir.FlattenedProperties(types, id)` returns a model's full shape: its members, one per wire name,
each with the declaration it takes, the declarations that one overrides, whether an instance must
carry it and whether its value is redacted, beside the names a `Requires` lists that no member
declares and the composition entries that name no model. Its rules:

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
  slot with its own type. The member takes every facet but whether it is required and whether it
  is redacted — type, `Presence`, visibility, default, docs — from the declaration it takes, and
  lists the ones it overrides, which is where a consumer conjoining more finds them. Nothing is
  reported: redeclaring is how a subtype narrows, and whether two declarations agree is a question
  about their types, which the flattening does not ask.
- *Requiredness.* A member is required where any of its declarations is `Required`, or any model
  the walk reads lists its wire name in `Requires`: JSON Schema requires what any conjunct
  requires, so requiredness only accumulates. A name a `Requires` lists that no member declares is
  undeclared, a member only the catch-all governs.
- *Redaction.* A member is redacted (`FlatProperty.Secret`) where any of its declarations asks for
  it: one that is `Secret`, or whose type is `Sensitive` or has a `Base` that reaches a sensitive
  type ([type graph](./type-graph.md)). A redaction only accumulates, as requiredness does, so a
  conjunct that redeclares a field another conjunct marks secret cannot print it in the clear: the
  member takes the later declaration's type and keeps the earlier one's redaction. A consumer reads
  the member's `Secret` and need not conjoin `Overridden` itself.
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
  of that model takes, the content's type for an encoding key, or the first variant declaring the
  part where that type is a union, so a consumer finds it on the member itself. The OpenAPI compiler
  keys a part through the flattening, and tags a discriminator by an own property, which the
  flattening takes. `pass.Validate` holds each only to a property the model
  exposes, not yet to the one it takes, since a flattening per model it asks about grows
  quadratically down a deep hierarchy, which its cost tests refuse.
- *Cycles and cost.* Each model and alias scalar is read once, so a cyclic composition is read to
  its fixpoint, each model of a cycle holding the members of all of them, and nothing is cut short:
  the walk needs no step budget and no recursion. Each type a redaction is read through is read once
  too, so a `Base` chain many declarations name costs one reading, and one that cycles ends where it
  repeats. It costs the declarations it returns plus the models, entries, aliases and types it
  passes, so an `irfacts` index could answer no faster, and it stays a function of `ir`.

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
target can express. An `allOf` none of whose conjuncts can make an object has nothing to compose, so
short of the limits [below](#an-allof-no-object-satisfies) it is a use of one of them.

## An allOf no object satisfies

A `Model` says its value is an object, and `allOf: [{type: integer}, {type: integer, format: int32}]`
admits none, so it is no composition. An `allOf` no conjunct can make an object lowers to one use of
the conjunct that decides it, short of the cases the OpenAPI compiler's rule
[`allof-refinement`](../compilers/openapi.md#allof-refinement) keeps composing, two `$ref`s or a union
beside the `allOf` among them: a reference to its `$ref`, a node at an inline branch's own pointer,
and an alias only where the position writes what no carrier holds.
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

What `S` says of the position rather than of its shape, its docs, deprecation, examples and what a
use binds, is said once, on the `Union` that stands there and the carriers using it, and a value
bound, which constrains both sides of the disjunction, on each variant. That holds whether `S`
writes it beside the `allOf` or on an inline branch of it, which conjoins the same way. Keeping such
a branch whole on each variant instead would let the source be rebuilt from any one variant, but it
would copy one fact onto as many nodes as there are variants, none of them where a reader of the
position looks, and tell apart two spellings that mean the same.

The variant models are anonymous and **synthesized**: no schema in the source occupies them, so
each takes an ID in a namespace of its own, derived from — but not equal to — the ID the branch
pointer would yield. Provenance still points at the branch, the nearest thing the source wrote.
Giving them the branch pointer's own ID would put two different types on one pointer: a `$ref` to
`…/oneOf/N` denotes the *branch schema*, and whichever of the two lowered first would take the
pointer from the other. That makes the IR depend on declaration order and can produce exactly the
`Union` the next paragraph forbids.

Distribution needs a referent per branch, because `Base`/`Mixins` conjoin by reference: a `$ref`
branch's target, which must resolve, or the node an inline branch lowers to at its own pointer,
which is no synthesized node and so takes that pointer's ID. A union that cannot supply a referent
for every branch is not distributed *at all* — never halfway, which would leave a `Union` whose
variants disagree about whether they carry `S` with nothing in the IR saying which. Those cases
degrade instead, enumerated by the OpenAPI compiler's rule
[`union-beside-structure`](../compilers/openapi.md#union-beside-structure).

Where `S` only restates what every branch already is, a `type: object` beside branches that are
all objects among it, the conjunction is the union itself, so the node is a `Union` of the
branches with nothing composed over them.

A discriminator written beside the union routes the variants, the composed ones included: a
mapping entry naming a branch's referent names the variant composed over it, and a route the
source implies by the referent's name, which the composed variant does not carry, is written into
the `Mapping`. Where a branch composes `S` in turn, a subtype of the hierarchy `S` is the base of,
the discriminator routes subtypes rather than variants, and a union of them would leave them no
base: `S` stays a `Model` holding the discriminator, and the OpenAPI compiler keeps the union
verbatim ([`union-beside-structure`](../compilers/openapi.md#union-beside-structure)).

## A union a composition names

A union a composition names by reference, rather than writing it beside the `allOf` or on one of its
inline branches, composes as any other referent does ([composition entries](#composition-entries)):
the `Model` names it as its `Base` or a `Mixin`, declares what the schema writes itself, and
distributes nothing.
`Stored: {allOf: [{$ref: Rule}, {properties: {id: …}}]}`, `Rule` a union of `Block` and `Log`, is a
`Model` whose `Base` is `Rule` and which declares `id`: `Stored ∧ Rule`, as the source writes it.
Each instance of `Stored` is an instance of one variant of `Rule` carrying `id` beside that
variant's members, so a discriminator on `Rule` says which by routing the instance's tag to the
variant it names, and no tag names `Stored`, which carries no `DiscriminatorValue`
([discriminator](#discriminator)). An emitter whose target has no intersection type lowers the
`Model` as the union it narrows, at its refiner
([inheritance](../emitters/refine/models.md#inheritance)), whether or not the union routes by a tag:
invariant 2 places a lowering to what a target can express there. A `Model` the union lists, as
`Shape: {oneOf: [Circle, Square]}` lists `Circle: {allOf: [Shape, …]}`, narrows nothing, each of
its instances being that variant.

Distributing it in the compiler instead, as a union written beside the `allOf` is distributed
[above](#composition-beside-a-union), would hand every consumer a node for each shape an instance
takes, and the tag a route to it. It was rejected on input the compiler must lower (spec
@`e411be7e`, morphic @`c74411086`):

- **It would repeat, and still leave the shape.** A variant a distribution composes names its
  branch's referent, which may itself be a union, and a schema composing a distributed one composes
  a union in turn: 15 of the golden spec's 48 models composing a union are composed variants, and
  `dlp_EntryWithSharedProfiles` composes `dlp_EntryWithUploadStatus`, which composes `dlp_Entry`.
  Each would distribute again, a schema the next reason keeps a `Model` would keep its union, and
  the compiler would hold a second copy of the refiner's mechanism.
- **A distant fact would decide it.** A schema a discriminator on one of its ancestors routes to
  stays a subtype of that ancestor (`ir.IsSubtype`), which no `Union` is: in
  `Cat: {allOf: [{$ref: Pet}, {$ref: Breeds}, …]}`, `Pet` routing `Cat` by its name, `Breeds` must
  stay a `Mixin`. Whether a schema distributed would turn on what another schema declares, as shape
  6 of [`union-beside-structure`](../compilers/openapi.md#union-beside-structure) lets it for a
  union written beside the `allOf`.
- **A shared union would be copied.** Each schema composing a union would carry its own copy of the
  union's variants and discriminator, the golden spec's most shared union ten times over, each
  composed variant a node minted per pair of schema and variant, and no node would say that the
  schema composes the union, as the source does.
