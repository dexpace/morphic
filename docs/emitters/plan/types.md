# TypePlan — per-type projections computed once

```go
type TypePlan struct {
    Flattened  []ir.PropID          // ir.FlattenedProperties over Base+Implements+Mixins+own: the
                                    // declaration each member takes, in layout order — COMPUTED
    Required   []ir.PropID          // the flattened properties an instance must carry: each one
                                    // a declaration marks Required or a Requires names
    Undeclared []string             // the names Requires lists that no flattened property declares,
                                    // in order: members only the catch-all governs
    Unflattened []ir.TypeID         // the composition entries naming no model, which Flattened
                                    // cannot hold: each lowered beside the properties, or degraded
    Union      UnionShape           // a union's wire shape, one of four (../refine/unions.md); "" = not a union
    Discrim    *DiscrimFacts        // a union's or hierarchy's discriminator, as variant IDs
    Open       bool                 // a union, polymorphic base or enum keeps a value no variant or
                                    // member recognizes rather than refusing it (policy.md,
                                    // ../refine/unions.md, ../refine/enums.md)
    Inclusive  bool                 // an any_of union two of whose variants can admit one value,
                                    // holding every variant a value admits (below); false otherwise
    Scalar     *ScalarChain         // a scalar's Base chain, walked once; nil = not a scalar
    Shapes     map[ir.Lifecycle]ModelShape // the wire shapes the document exercises (below)
    Alias      ir.TypeID            // the type every reference to this one is emitted as, this
                                    // one emitting nothing: a decorator's end (../refine/decorators.md), or a dedup
                                    // alias's target once that pass (architecture.md §2.2) and its
                                    // record (docs/ir/open-questions.md, question 2) exist; "" = itself
    Reach      []ir.GroupID         // the top-level operation groups whose operations reach this
                                    // type, sorted; empty = none does (../refine/output-units.md)
    Inferred   map[string]ir.Provenance
}

// UnionShape is the wire shape a union's serializer reads, from Exclusive, WireTagged and
// Discriminator together (docs/ir/unions.md#tag-modes).
type UnionShape string              // "one_of" | "any_of" | "discriminated" | "key_tagged"

type ScalarChain struct {           // a Scalar's Base chain walked to its root (../refine/scalars.md)
    Root        ir.TypeID           // the chain's first node that is not a Scalar: a primitive, or
                                    // what an alias stands over (docs/ir/scalars.md); "" = opaque
    Constraints *ir.Constraints     // merged along the chain, decimal throughout (../refine/constraints.md)
    Encoding    *ir.Encoding        // the encoding declared nearest this scalar; nil = the root's
}

type DiscrimFacts struct {          // Mapping/Default resolved to TypeIDs, never names (INV3)
    Locator  DiscrimLocator         // property | property_name | index
    WireKey  string                 // wire name of the tag property ("" when Locator == index)
    Index    *int                   // positional tag element (Erlang tagged tuples); nil otherwise
    Mapping  map[string]ir.TypeID   // wire tag value → variant ID
    Default  ir.TypeID              // variant for absent/unrecognized tag; "" = none
    Envelope string                 // "" = inline tag | "object" = {kind,value} wrapper
    EnvelopeValueName string        // wire name of the envelope value property (Envelope=="object")
}
type DiscrimLocator string          // "property" | "property_name" | "index"
```

Note what `TypePlan` does *not* do: it does not choose Go's sealed interface vs a native union, does
not case anything, does not resolve reserved words, and does not copy a fact the IR states outright
([plan or refine](plan-or-refine.md)). `Flattened` is the *computed* flattening; the IR keeps
`Base`, `Implements` and `Mixins` distinct beside it, so a class-based target can map `Base→extends`
/ `Implements→interfaces` / `Mixins→inline` where the three first targets flatten
([inheritance](../refine/models.md#inheritance)). The plan does not decide which — that is
per-language ([refine](../refine/pipeline.md)).

`Flattened`, `Required`, `Undeclared` and `Unflattened` are one `ir.FlattenedProperties` walk, by
the rules [models](../../ir/models.md#the-flattening) states: its order, which declaration of a
redeclared member is taken, and when a member is required. A flattened property is required where
any of its declarations says so, an overridden one included, or where this model or one it composes
lists its wire name in `Requires`, as `allOf: [{$ref: User}, {required: [id]}]` makes `User`'s `id`
required on the composed model. A name `Requires` lists that no flattened property declares is
`Undeclared`: a member only the catch-all governs, whose value the catch-all types —
`AdditionalProps.Value`, or the top type where the model has no catch-all or scopes values by
`Patterns`, since which pattern governs a name is ECMA-262's to say and no target's matcher is held
to it. A conjunct that is no model, as `Tags` is in `allOf: [{$ref: Pet}, {$ref: Tags}]` when
`Tags` is a list, is in `Unflattened` as the entry names it: the declaration over `Flattened` does
not hold what it admits, so each refiner lowers it or degrades with a code of its own.

## Variants that can share a value

`Inclusive` is true on an `any_of` union when two of its variants can admit one value, and false on
every other shape. What admitting a value means is in
[unions](../refine/unions.md#a-value-several-variants-admit). The plan proves two variants disjoint,
admitting no value in common, by these rules, and a pair none of them separates can share one:

- **Kinds.** Their JSON kinds have none in common. A variant's kinds are its type's: object for a
  model or a map, array for a list, a tuple or a `Positional` model, its wire kind for a primitive,
  its values' for an enum or a literal, its variants' for a union, its root's for a scalar, its
  base's for a model that only stands over one, and every kind for `any`. A nullable reference adds
  `null`, and an integer is a number too. A variant's own `null` is the union's
  ([unions](../refine/unions.md#a-null-a-variant-admits)), so the rules leave it out, and a variant
  admitting only `null` takes no part.
- **Values.** Each admits finitely many values, as a closed enum, a literal or a union of them does,
  and no value is in both, compared as JSON values.
- **Members.** Both are models laid out by name, read through `Flattened`, and one requires a member
  the other either does not declare and is closed, or declares at a position disjoint from it by
  these rules. A pair already being compared counts as sharing, so a cycle of models ends the walk.

The rules are sound, not complete. A pair they leave sharing may still admit no common value, as a
`date` and a `uuid` string do, and is then inclusive: it holds every value correctly, though more
loosely than a closed sum would. `Constraints` take no part, since admitting a value leaves them to
validation, so two variants that differ only in a bound do share values.

On the golden spec (spec @319dc756, morphic @b206dc99), 515 unions are untagged, not exclusive, and
have two or more variants. These rules prove 419 of them disjoint, and those lower as a closed sum.
Of the 96 left, 72 have two variants that both admit an object, such as the 16 identity providers
an Access listing returns. Each refines one base whose `type` enum lists all 16, and each `config`
is open with no required member, so nothing separates them: one provider's value is admitted by
every other whose `config` declares none of its members at another type.

## ModelShape — visibility projection

One logical model produces N wire shapes across the lifecycle. `ModelShape(model, lifecycle)` is a
**computed traversal in the plan layer**; the IR stores only the single logical model plus visibility
facts and never the projected variants (INV2; [properties](../../ir/properties.md#visibility)). The
plan emits one `ModelShape` per lifecycle the document actually exercises, in the type's
`TypePlan.Shapes`, so a refiner knows exactly which shapes to build: those keys are the projections
the type needs, and nothing else lists them.

```go
type ModelShape struct {
    Props   []ir.PropID  // properties visible in this lifecycle
    Patch   bool         // PATCH shape: properties are implicitly optional unless the binding disables it
    Inferred map[string]ir.Provenance
}
```

`Props` are drawn from `TypePlan.Flattened`, so a composed model's shapes hold what it inherits and
mixes in, each member visible as the declaration it takes says
([models](../../ir/models.md#the-flattening)). `readOnly → Only:[read, delete, query]`;
`writeOnly → Only:[create, update]` ([properties](../../ir/properties.md#visibility));
`Visibility.None` → excluded from every projection (distinct from the zero value); PATCH flips
properties optional unless `HTTPBinding.PatchImplicitOptionality` points at `false` (nil is the
protocol default, which flips them), and a patch shape never carries a property's default onto the
wire ([values](../refine/values.md)); operation-level `ParameterVisibility` / `ReturnTypeVisibility`
override which filter applies. Unknown visibility classes are opaque filters evaluated per class
([properties](../../ir/properties.md#visibility)).

## A model with lifecycle visibility

IR: `Model{ Properties:[id (readOnly), name (required), email, password (writeOnly, Secret)],
Additional:"closed" }`.

`ModelShape(User, read)` = {id,name,email}; `ModelShape(User, create)` = {name,email,password}.
`Flattened` walks `Base`, `Implements` and `Mixins` (none here). That `User` is both written and
read is the derived-fact index's, `Facts.Usage(User)` = Input|Output, which no plan field copies
([plan or refine](plan-or-refine.md)).

What refine and emit build from it: [Go](../targets/go/models.md#user-in-go),
[Kotlin](../targets/kotlin/models.md).
